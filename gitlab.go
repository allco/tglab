package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const mrFields = `
  iid title webUrl draft updatedAt sourceBranch targetBranch diffHeadSha
  author { username }
  project { fullPath }
  headPipeline { status }
  approved approvalsLeft approvalsRequired
  conflicts detailedMergeStatus
  mergeTrainIndex autoMergeEnabled autoMergeStrategy
  resolvableDiscussionsCount resolvedDiscussionsCount
  labels(first: 100) { nodes { title color textColor } }`

// MRs opened by the current user, in one project or (project null) everywhere.
const mineQuery = `query($project: String, $after: String) {
  currentUser {
    username
    authoredMergeRequests(projectPath: $project, state: opened, first: 100, after: $after, sort: UPDATED_DESC) {
      pageInfo { hasNextPage endCursor }
      nodes {` + mrFields + ` }
    }
  }
}`

// Every open MR in a project, whoever opened it.
const allQuery = `query($project: ID!, $after: String) {
  currentUser { username }
  project(fullPath: $project) {
    mergeRequests(state: opened, first: 100, after: $after, sort: UPDATED_DESC) {
      pageInfo { hasNextPage endCursor }
      nodes {` + mrFields + ` }
    }
  }
}`

// maxPages bounds a refresh at 1000 MRs per project.
const maxPages = 10

type MR struct {
	IID          string    `json:"iid"`
	Title        string    `json:"title"`
	WebURL       string    `json:"webUrl"`
	Draft        bool      `json:"draft"`
	UpdatedAt    time.Time `json:"updatedAt"`
	SourceBranch string    `json:"sourceBranch"`
	TargetBranch string    `json:"targetBranch"`
	DiffHeadSha  string    `json:"diffHeadSha"`
	Author       struct {
		Username string `json:"username"`
	} `json:"author"`
	Project struct {
		FullPath string `json:"fullPath"`
	} `json:"project"`
	HeadPipeline *struct {
		Status string `json:"status"`
	} `json:"headPipeline"`
	Approved            bool   `json:"approved"`
	ApprovalsLeft       int    `json:"approvalsLeft"`
	ApprovalsRequired   int    `json:"approvalsRequired"`
	Conflicts           bool   `json:"conflicts"`
	DetailedMergeStatus string `json:"detailedMergeStatus"`
	MergeTrainIndex     *int   `json:"mergeTrainIndex"` // zero-based, nil when not on a train
	AutoMergeEnabled    bool   `json:"autoMergeEnabled"`
	AutoMergeStrategy   string `json:"autoMergeStrategy"`
	ResolvableThreads   int    `json:"resolvableDiscussionsCount"`
	ResolvedThreads     int    `json:"resolvedDiscussionsCount"`
	Labels              struct {
		Nodes []Label `json:"nodes"`
	} `json:"labels"`
}

type Label struct {
	Title     string `json:"title"`
	Color     string `json:"color"`
	TextColor string `json:"textColor"`
}

func (m MR) PipelineStatus() string {
	if m.HeadPipeline == nil {
		return ""
	}
	return m.HeadPipeline.Status
}

func (m MR) UnresolvedThreads() int {
	return m.ResolvableThreads - m.ResolvedThreads
}

// Signature changes whenever something worth noticing about the MR changes.
func (m MR) Signature() string {
	labels := make([]string, len(m.Labels.Nodes))
	for i, l := range m.Labels.Nodes {
		labels[i] = l.Title
	}
	return fmt.Sprintf("%s|%v|%d|%d|%v|%s|%s|%s|%s",
		m.PipelineStatus(), m.Approved, m.ApprovalsLeft, m.UnresolvedThreads(), m.Conflicts, m.DetailedMergeStatus, m.Title,
		strings.Join(labels, ","), statusCell(m))
}

type Client struct {
	baseURL string // https://<host>
	http    *http.Client

	mu    sync.Mutex
	token string
	// Set when the token comes from glab: reload re-reads it, renew makes glab renew it first.
	reload func() (string, error)
	renew  func(context.Context) (string, error)
}

func NewClient(host, token string, fromGlab bool) *Client {
	c := &Client{baseURL: "https://" + host, token: token, http: &http.Client{Timeout: 30 * time.Second}}
	if fromGlab {
		c.reload = func() (string, error) { return glabToken(host) }
		c.renew = func(ctx context.Context) (string, error) { return renewGlabToken(ctx, host) }
	}
	return c
}

var errUnauthorized = errors.New("401 Unauthorized")

func (c *Client) currentToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

func (c *Client) setToken(t string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = t
}

type mrPage struct {
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
	Nodes []MR `json:"nodes"`
}

// graphql runs one query. A glab token rejected as expired is renewed through glab and the query
// retried once.
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, data any) error {
	err := c.post(ctx, query, vars, data)
	if c.renew == nil || !errors.Is(err, errUnauthorized) {
		return err
	}
	if t, rerr := c.renew(ctx); rerr == nil && t != "" {
		c.setToken(t)
		err = c.post(ctx, query, vars, data)
	}
	if errors.Is(err, errUnauthorized) {
		return fmt.Errorf("%w (the glab login may have expired: run `glab auth login`)", err)
	}
	return err
}

func (c *Client) post(ctx context.Context, query string, vars map[string]any, data any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.currentToken())

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("GitLab API: %w", errUnauthorized)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitLab API: %s", resp.Status)
	}

	out := struct {
		Data   any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}{Data: data}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(out.Errors) > 0 {
		return fmt.Errorf("GitLab API: %s", out.Errors[0].Message)
	}
	return nil
}

// fetchMine returns the current user's username and their open MRs in project (everywhere if nil).
func (c *Client) fetchMine(ctx context.Context, project *string) (string, []MR, error) {
	var me string
	var mrs []MR
	var after *string
	for range maxPages {
		var data struct {
			CurrentUser *struct {
				Username              string `json:"username"`
				AuthoredMergeRequests mrPage `json:"authoredMergeRequests"`
			} `json:"currentUser"`
		}
		if err := c.graphql(ctx, mineQuery, map[string]any{"project": project, "after": after}, &data); err != nil {
			return "", nil, err
		}
		if data.CurrentUser == nil {
			return "", nil, fmt.Errorf("GitLab API: not authenticated")
		}
		me = data.CurrentUser.Username
		page := data.CurrentUser.AuthoredMergeRequests
		mrs = append(mrs, page.Nodes...)
		if !page.PageInfo.HasNextPage {
			break
		}
		after = &page.PageInfo.EndCursor
	}
	return me, mrs, nil
}

// fetchAll returns the current user's username and every open MR in project.
func (c *Client) fetchAll(ctx context.Context, project string) (string, []MR, error) {
	var me string
	var mrs []MR
	var after *string
	for range maxPages {
		var data struct {
			CurrentUser *struct {
				Username string `json:"username"`
			} `json:"currentUser"`
			Project *struct {
				MergeRequests mrPage `json:"mergeRequests"`
			} `json:"project"`
		}
		if err := c.graphql(ctx, allQuery, map[string]any{"project": project, "after": after}, &data); err != nil {
			return "", nil, err
		}
		if data.CurrentUser == nil {
			return "", nil, fmt.Errorf("GitLab API: not authenticated")
		}
		if data.Project == nil {
			return "", nil, fmt.Errorf("project not found or no access")
		}
		me = data.CurrentUser.Username
		page := data.Project.MergeRequests
		mrs = append(mrs, page.Nodes...)
		if !page.PageInfo.HasNextPage {
			break
		}
		after = &page.PageInfo.EndCursor
	}
	return me, mrs, nil
}

// FetchMRs returns every open MR in the given projects, most recently updated first, plus the
// current user's username. With no projects it returns the user's own open MRs everywhere, since
// every MR on the host is not a useful list.
func (c *Client) FetchMRs(ctx context.Context, projects []string) ([]MR, string, error) {
	// glab may have renewed its token since the last refresh, e.g. when another glab command ran.
	if c.reload != nil {
		if t, err := c.reload(); err == nil {
			c.setToken(t)
		}
	}
	if len(projects) == 0 {
		me, mrs, err := c.fetchMine(ctx, nil)
		sortLabels(mrs)
		return mrs, me, err
	}
	var me string
	var mrs []MR
	for _, p := range projects {
		var got []MR
		var err error
		me, got, err = c.fetchAll(ctx, p)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", p, err)
		}
		mrs = append(mrs, got...)
	}
	sort.SliceStable(mrs, func(i, j int) bool { return mrs[i].UpdatedAt.After(mrs[j].UpdatedAt) })
	sortLabels(mrs)
	return mrs, me, nil
}

// sortLabels orders each MR's labels alphabetically, ignoring case.
func sortLabels(mrs []MR) {
	for i := range mrs {
		labels := mrs[i].Labels.Nodes
		sort.SliceStable(labels, func(a, b int) bool {
			return strings.ToLower(labels[a].Title) < strings.ToLower(labels[b].Title)
		})
	}
}
