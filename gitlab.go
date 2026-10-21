package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
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
	host  string
	token string
	http  *http.Client
}

func NewClient(host, token string) *Client {
	return &Client{host: host, token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

type mrPage struct {
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
	Nodes []MR `json:"nodes"`
}

func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, data any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+c.host+"/api/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
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
