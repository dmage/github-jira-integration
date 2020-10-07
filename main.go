package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/andygrunwald/go-jira"
	"github.com/google/go-github/v32/github"
	"k8s.io/klog/v2"
)

type OwnerName struct {
	Owner string
	Name  string
}

var repositories = []OwnerName{
	{Owner: "openshift", Name: "api"},
	{Owner: "openshift", Name: "cluster-image-registry-operator"},
	{Owner: "openshift", Name: "docker-distribution"},
	{Owner: "openshift", Name: "image-registry"},
	{Owner: "openshift", Name: "openshift-apiserver"},
	{Owner: "openshift", Name: "origin"},
}

var jiraProjects = []string{
	"IR",
}

func getEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		klog.Exitf("The environment variable %s is not set or empty. Please set it and try again.", name)
	}
	return value
}

func pullRequestLink(pr *github.PullRequest) string {
	return fmt.Sprintf("https://github.com/%s/pull/%d", pr.Base.Repo.GetFullName(), pr.GetNumber())
}

func pullRequestLinkTitle(pr *github.PullRequest) string {
	return fmt.Sprintf("%s#%d", pr.Base.Repo.GetFullName(), pr.GetNumber())
}

func linkPullRequestToIssue(jiraClient *jira.Client, pr *github.PullRequest, issueKey string) {
	klog.V(3).Infof("Checking if %s is linked to %s...", pullRequestLinkTitle(pr), issueKey)

	title := pr.GetTitle()
	if strings.HasPrefix(title, issueKey+": ") {
		title = title[len(issueKey+": "):]
	}

	links, _, err := jiraClient.Issue.GetRemoteLinks(issueKey)
	//issue, _, err := jiraClient.Issue.Get(issueKey, nil)
	if err != nil {
		klog.Fatal(err)
	}

	remoteURL := pullRequestLink(pr)
	remoteTitle := fmt.Sprintf("%s: %s", pullRequestLinkTitle(pr), title)

	for _, link := range *links {
		if link.Object.URL == remoteURL {
			klog.V(3).Infof("%s is already linked to %s", pullRequestLinkTitle(pr), issueKey)
			return
		}
	}

	klog.V(1).Infof("Linking the pull request %s to the issue %s...", pullRequestLinkTitle(pr), issueKey)

	link := &jira.RemoteLink{
		Object: &jira.RemoteLinkObject{
			URL:   remoteURL,
			Title: remoteTitle,
			Icon: &jira.RemoteLinkIcon{
				Url16x16: "https://github.com/favicon.ico",
				Title:    "GitHub",
			},
		},
	}

	req, _ := jiraClient.NewRequest("POST", "rest/api/2/issue/"+issueKey+"/remotelink", link)
	resp, err := jiraClient.Do(req, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		klog.Fatal(err)
	}
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	baseURL := getEnv("JIRA_BASE_URL")
	tp := jira.BasicAuthTransport{
		Username: getEnv("JIRA_USERNAME"),
		Password: getEnv("JIRA_PASSWORD"),
	}

	keyPattern := `(?:`
	for i, projectKey := range jiraProjects {
		if i != 0 {
			keyPattern += `|`
		}
		keyPattern += regexp.QuoteMeta(projectKey)
	}
	keyPattern += `)-[0-9]+`
	keyRegexp, err := regexp.Compile(`(` + keyPattern + `): `)
	if err != nil {
		klog.Fatal(err)
	}

	ctx := context.Background()

	jiraClient, err := jira.NewClient(tp.Client(), baseURL)
	if err != nil {
		klog.Fatal(err)
	}

	githubClient := github.NewClient(nil)

	for _, repo := range repositories {
		klog.V(2).Infof("Analyzing github repository %s/%s...", repo.Owner, repo.Name)
		prs, _, err := githubClient.PullRequests.List(ctx, repo.Owner, repo.Name, &github.PullRequestListOptions{
			State:     "all",
			Sort:      "updated",
			Direction: "desc",
			ListOptions: github.ListOptions{
				Page:    1,
				PerPage: 100,
			},
		})
		if err != nil {
			klog.Fatal(err)
		}

		for _, pr := range prs {
			title := *pr.Title
			match := keyRegexp.FindStringSubmatch(title)
			if match == nil {
				continue
			}
			issueKey := match[1]
			linkPullRequestToIssue(jiraClient, pr, issueKey)
		}
	}
}
