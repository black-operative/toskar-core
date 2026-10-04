package connectors

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/yeixio/yggdrasil-core/internal/tools"
)

// GitHub reads issues and pull requests, and can comment on them.
type GitHub struct{}

func (GitHub) ID() string   { return "github" }
func (GitHub) Name() string { return "GitHub" }
func (GitHub) Description() string {
	return "Search and read issues and pull requests, and comment on them."
}
func (GitHub) Scopes() string {
	return "Create a fine-grained personal access token limited to the repositories you want. " +
		"Read-only access to Issues and Pull requests is enough to search and read. " +
		"Add Issues and Pull requests: Read and write only if Toskar should comment."
}

func (GitHub) Fields() []Field {
	return []Field{
		{Key: "token", Label: "Personal access token", Secret: true, Placeholder: "github_pat_…"},
		{Key: "api_url", Label: "API address", Optional: true, Placeholder: "https://api.github.com",
			Help: "Only for GitHub Enterprise Server."},
	}
}

func ghBase(cred Credential) string {
	if u := strings.TrimRight(cred["api_url"], "/"); u != "" {
		return u
	}
	return "https://api.github.com"
}

func ghHeader(cred Credential) map[string]string {
	return map[string]string{
		"Authorization":        "Bearer " + cred["token"],
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}
}

func (GitHub) Check(ctx context.Context, c *http.Client, cred Credential) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	if err := call(ctx, c, http.MethodGet, ghBase(cred)+"/user", ghHeader(cred), nil, &user); err != nil {
		return "", err
	}
	return "@" + user.Login, nil
}

var repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func ghRepo(args map[string]any) (string, int, error) {
	repo, n := str(args, "repo"), num(args, "number")
	repo = strings.TrimSuffix(strings.TrimPrefix(repo, "https://github.com/"), ".git")
	if !repoRe.MatchString(repo) {
		return "", 0, fmt.Errorf("repo must look like owner/name")
	}
	if n <= 0 {
		return "", 0, fmt.Errorf("number must be an issue or pull request number")
	}
	return repo, n, nil
}

type ghIssue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	UpdatedAt   string `json:"updated_at"`
	Comments    int    `json:"comments"`
	RepoURL     string `json:"repository_url"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (i ghIssue) view() map[string]any {
	kind := "issue"
	if i.PullRequest != nil {
		kind = "pull request"
	}
	repo := i.RepoURL
	if k := strings.Index(repo, "/repos/"); k >= 0 {
		repo = repo[k+len("/repos/"):]
	}
	return map[string]any{
		"repo": repo, "number": i.Number, "kind": kind, "title": i.Title, "state": i.State,
		"url": i.HTMLURL, "author": i.User.Login, "updated_at": i.UpdatedAt, "comments": i.Comments,
	}
}

func (GitHub) Tools() []Tool {
	return []Tool{
		{
			Def: tools.Definition{ID: "github.search", Name: "Search GitHub", Capability: "github", Risk: tools.RiskRead, DefaultPolicy: tools.PolicyAllow,
				Description: "Search GitHub issues and pull requests the connected account can see. The query uses GitHub search syntax, such as \"repo:owner/name is:open label:bug\".",
				Schema:      `{"query":"string"}`},
			Run: func(ctx context.Context, c *http.Client, cred Credential, args map[string]any) (map[string]any, error) {
				q := str(args, "query")
				if q == "" {
					return nil, fmt.Errorf("query is required")
				}
				var res struct {
					Total int       `json:"total_count"`
					Items []ghIssue `json:"items"`
				}
				u := ghBase(cred) + "/search/issues?per_page=10&q=" + url.QueryEscape(q)
				if err := call(ctx, c, http.MethodGet, u, ghHeader(cred), nil, &res); err != nil {
					return nil, err
				}
				items := make([]any, 0, len(res.Items))
				for _, it := range res.Items {
					items = append(items, it.view())
				}
				return map[string]any{"total": res.Total, "results": items}, nil
			},
		},
		{
			Def: tools.Definition{ID: "github.issue", Name: "Read GitHub issue", Capability: "github", Risk: tools.RiskRead, DefaultPolicy: tools.PolicyAllow,
				Description: "Read one GitHub issue or pull request, with its recent comments.",
				Schema:      `{"repo":"owner/name","number":"integer"}`},
			Run: func(ctx context.Context, c *http.Client, cred Credential, args map[string]any) (map[string]any, error) {
				repo, n, err := ghRepo(args)
				if err != nil {
					return nil, err
				}
				base := fmt.Sprintf("%s/repos/%s/issues/%d", ghBase(cred), repo, n)
				var issue ghIssue
				if err := call(ctx, c, http.MethodGet, base, ghHeader(cred), nil, &issue); err != nil {
					return nil, err
				}
				var comments []struct {
					Body      string `json:"body"`
					CreatedAt string `json:"created_at"`
					User      struct {
						Login string `json:"login"`
					} `json:"user"`
				}
				if err := call(ctx, c, http.MethodGet, base+"/comments?per_page=20", ghHeader(cred), nil, &comments); err != nil {
					return nil, err
				}
				out := issue.view()
				out["body"] = clip(issue.Body, 4000)
				list := make([]any, 0, len(comments))
				for _, cm := range comments {
					list = append(list, map[string]any{"author": cm.User.Login, "created_at": cm.CreatedAt, "body": clip(cm.Body, 1500)})
				}
				out["recent_comments"] = list
				return out, nil
			},
		},
		{
			Def: tools.Definition{ID: "github.comment", Name: "Comment on GitHub", Capability: "github", Risk: tools.RiskWrite, DefaultPolicy: tools.PolicyAsk,
				Description: "Post a comment on a GitHub issue or pull request as the connected account.",
				Schema:      `{"repo":"owner/name","number":"integer","body":"string"}`},
			Run: func(ctx context.Context, c *http.Client, cred Credential, args map[string]any) (map[string]any, error) {
				repo, n, err := ghRepo(args)
				if err != nil {
					return nil, err
				}
				body := str(args, "body")
				if body == "" {
					return nil, fmt.Errorf("body is required")
				}
				var res struct {
					HTMLURL string `json:"html_url"`
				}
				u := fmt.Sprintf("%s/repos/%s/issues/%d/comments", ghBase(cred), repo, n)
				if err := call(ctx, c, http.MethodPost, u, ghHeader(cred), map[string]string{"body": body}, &res); err != nil {
					return nil, err
				}
				return map[string]any{"url": res.HTMLURL, "repo": repo, "number": n}, nil
			},
		},
	}
}
