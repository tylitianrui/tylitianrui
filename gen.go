package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

//go:generate go run gen.go

// googleSourceGitHub holds mapping of
// a Go Google Git repository name https://go.googlesource.com/<GoogleSourceRepo>
// to GitHub owner name https://github.com/<GitHubOwnerName>.
type googleSourceGitHub struct {
	GoogleSourceRepo string
	GitHubOwnerName  string
}

// googleGitHubRepos are Go Google Git repositories I have ever contributed to.
var googleGitHubRepos = []googleSourceGitHub{
	{"build", "golang/build"},
	{"go", "golang/go"},
	{"net", "golang/net"},
	{"mod", "golang/mod"},
	{"protobuf", "protocolbuffers/protobuf-go"},
	{"tools", "golang/tools"},
	{"text", "golang/text"},
	{"vulndb", "golang/vulndb"},
	{"website", "golang/website"},
}

// additionalGitHubRepos holds GitHub repositories I have contributed to with
// label 'Closed'. Pull requests marked as "Closed" but commits from them moved
// to repo's main branch. This happens when a main repository is in Gerrit
// and GitHub is a mirror.
var additionalGitHubRepos = []string{
	"cue-lang/cue", // https://review.gerrithub.io/q/project:cue-lang%252Fcue
	"cognitedata/cognite-sdk-python",
}

func main() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("env variable 'GITHUB_TOKEN' must be non-empty")
	}

	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	httpClient := oauth2.NewClient(context.Background(), src)
	client := githubv4.NewClient(httpClient)

	allPullRequests, err := PullRequests(context.Background(), client)
	if err != nil {
		log.Fatalf("Failed to get merged pull requests: %v\n", err)
	}
	log.Printf("Total pull request: %d\n", len(allPullRequests))

	repositoryStars := map[string]int{}
	for _, pr := range allPullRequests {
		ownerName := string(pr.Node.Repository.NameWithOwner)
		if ownRepo(ownerName) {
			log.Printf("Skipping own repo: %s\n", ownerName)
			continue
		}
		if !pr.Node.Merged {
			log.Printf("Skipping not merged repo: %s\n", ownerName)
			continue
		}

		repositoryStars[ownerName] = int(pr.Node.Repository.StargazerCount)
	}

	for _, googleGithub := range googleGitHubRepos {
		ownerName := googleGithub.GitHubOwnerName
		starsCount, err := RepositoryStarsCount(context.Background(), client, ownerName)
		if err != nil {
			log.Printf("Failed to get repository %q stars: %v", ownerName, err)
			starsCount = 1000
		}
		repositoryStars[ownerName] = starsCount
	}

	for _, ownerName := range additionalGitHubRepos {
		starsCount, err := RepositoryStarsCount(context.Background(), client, ownerName)
		if err != nil {
			log.Printf("Failed to get repository %q stars: %v", ownerName, err)
			starsCount = 100
		}
		repositoryStars[ownerName] = starsCount
	}

	type repository struct {
		OwnerName string
		StarCount int
	}

	repositories := make([]repository, 0, len(repositoryStars))
	for ownerName, star := range repositoryStars {
		repositories = append(repositories, repository{
			OwnerName: ownerName,
			StarCount: star,
		})
	}

	sort.Slice(repositories, func(i, j int) bool {
		return repositories[i].StarCount > repositories[j].StarCount
	})

	log.Printf("Total contributed projects: %d\n", len(repositories))

	contribFile, err := os.Create("CONTRIBUTIONS.md")
	if err != nil {
		log.Fatalf("Create file: %v", err)
	}
	defer func() {
		if err := contribFile.Close(); err != nil {
			log.Fatalf("Failed to close contrib file: %v\n", err)
		}
	}()
	_, _ = contribFile.WriteString(`<!---
Code generated by gen.go; DO NOT EDIT.

To update the doc run:
GITHUB_TOKEN=<YOUR_TOKEN> go generate ./...
-->

# Open Source Projects I've Ever Contributed
`)

	_, _ = contribFile.WriteString(`
## Go Google Git Repositories

_links pointed to a log with my contributions_

`)
	for _, repo := range googleGitHubRepos {
		line := fmt.Sprintf("* [%[1]s](https://go.googlesource.com/%[1]s/+log?author=tylitianrui)\n", repo.GoogleSourceRepo)
		_, _ = contribFile.WriteString(line)
	}

	_, _ = contribFile.WriteString(`
## GitHub Projects

_sorted by stars descending_

`)
	for _, repo := range repositories {
		line := fmt.Sprintf("* [%[1]s](https://github.com/%[1]s)\n", repo.OwnerName)
		_, _ = contribFile.WriteString(line)
	}
}

type edgePullRequest struct {
	Node struct {
		Repository struct {
			NameWithOwner  githubv4.String
			StargazerCount githubv4.Int
		}
		Merged githubv4.Boolean
		Closed githubv4.Boolean
	}
}

func PullRequests(ctx context.Context, client *githubv4.Client) ([]edgePullRequest, error) {
	var pullRequests []edgePullRequest
	variables := map[string]any{
		"after": (*githubv4.String)(nil),
	}

	for {
		var queryPullRequest struct {
			Viewer struct {
				PullRequests struct {
					PageInfo struct {
						EndCursor   githubv4.String
						HasNextPage bool
					}
					TotalCount githubv4.Int
					Edges      []edgePullRequest
				} `graphql:"pullRequests(states: [MERGED, CLOSED], orderBy:{field: CREATED_AT, direction: ASC}, first:100, after: $after)"`
			}
		}

		if err := client.Query(ctx, &queryPullRequest, variables); err != nil {
			return nil, fmt.Errorf("query: %w", err)
		}
		pullRequests = append(pullRequests, queryPullRequest.Viewer.PullRequests.Edges...)
		if !queryPullRequest.Viewer.PullRequests.PageInfo.HasNextPage {
			break
		}
		variables["after"] = queryPullRequest.Viewer.PullRequests.PageInfo.EndCursor
	}

	return pullRequests, nil
}

func RepositoryStarsCount(ctx context.Context, client *githubv4.Client, ownerName string) (int, error) {
	spl := strings.Split(ownerName, "/")
	if len(spl) != 2 {
		return 0, fmt.Errorf("repo %s must have format 'owner/name'", ownerName)
	}
	owner, name := spl[0], spl[1]

	variables := map[string]any{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(name),
	}

	var queryRepository struct {
		Repository struct {
			StargazerCount githubv4.Int
		} `graphql:"repository(owner: $owner, name: $name)"`
	}

	if err := client.Query(ctx, &queryRepository, variables); err != nil {
		return 0, fmt.Errorf("query: %w", err)
	}

	return int(queryRepository.Repository.StargazerCount), nil
}

// ownRepo returns true if merged to my github.com/tylitianrui account.
func ownRepo(ownerName string) bool {
	return strings.HasPrefix(ownerName, "tylitianrui/")
}
