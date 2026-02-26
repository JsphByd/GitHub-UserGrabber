package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

// ── ANSI colors ──────────────────────────────────────────────────────────────

const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorBlue   = "\033[34m"
)

func header(s string) { fmt.Printf("\n%s%s=== %s ===%s\n", colorBold, colorCyan, s, colorReset) }
func info(f string, a ...any) {
	fmt.Printf("  %s"+f+"%s\n", append([]any{colorGreen}, append(a, colorReset)...)...)
}
func warn(f string, a ...any) {
	fmt.Printf("  %s"+f+"%s\n", append([]any{colorYellow}, append(a, colorReset)...)...)
}
func bullet(f string, a ...any) { fmt.Printf("    • "+f+"\n", a...) }

// ── Data structures ───────────────────────────────────────────────────────────

type OrgData struct {
	Members         []string
	Followers       []string
	Repos           []string
	KeywordUsers    []string // users with keywords found in bio/company
	FollowerNetwork []string // followers-of-members and followers-of-followers
	CommitUsers     []string // verified GitHub logins from commit history
	ProjectUsers    []string // users found in org projects
	IssueUsers      []string // users found in repo issues
	UnverifiedUsers []string // git author names not linked to a GitHub account
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	orgsFile    := flag.String("o", "", "Path to file containing GitHub organization names (one per line)")
	kwFile      := flag.String("k", "", "Path to file containing keywords to filter users (one per line)")
	pat         := flag.String("t", "", "GitHub Personal Access Token (required to avoid rate limiting)")
	scanFofM    := flag.Bool("fm", false, "Also scan followers of org members")
	scanFofF    := flag.Bool("ff", false, "Also scan followers of org followers")
	scanCommits := flag.Bool("fc", false, "Also scan commit history of org repositories for contributors")
	scanProjects := flag.Bool("fp", false, "Also scan org projects for new users")
	scanIssues  := flag.Bool("fi", false, "Also scan repository issues for new users")
	scanAll     := flag.Bool("fa", false, "Scan everything: members, followers, followers-of-members/followers, projects, commits, and issues")
	maxPages    := flag.Int("p", 0, "Max number of pages to fetch per API call (0 = unlimited)")
	scanName    := flag.String("n", "", "Filename prefix to save results under")
	flag.Parse()

	if *orgsFile == "" || *kwFile == "" {
		fmt.Fprintf(os.Stderr, "%sUsage: github-scanner -o <orgs_file> -k <keywords_file> [-t <github_pat>] [-fm] [-ff] [-fc] [-fp] [-fi] [-fa] [-p <max_pages>] [-n <scan_name>]%s\n", colorRed, colorReset)
		flag.PrintDefaults()
		os.Exit(1)
	}

	// ── Resolve -fa into individual flags ─────────────────────────────────────
	if *scanAll {
		*scanFofM     = true
		*scanFofF     = true
		*scanCommits  = true
		*scanProjects = true
		*scanIssues   = true
		info("Scan-all enabled — activating: -fm -ff -fc -fp -fi")
	}

	orgs, err := readLines(*orgsFile)
	fatalIf("reading orgs file", err)

	keywords, err := readLines(*kwFile)
	fatalIf("reading keywords file", err)

	var client *github.Client
	if *pat != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: *pat})
		tc := oauth2.NewClient(context.Background(), ts)
		client = github.NewClient(tc)
		info("Authenticated with PAT — rate limit: 5,000 req/hour")
	} else {
		client = github.NewClient(nil)
		warn("No PAT provided — using unauthenticated requests (60 req/hour). Use -t to authenticate.")
	}
	ctx := context.Background()

	orgMap := make(map[string]*OrgData, len(orgs))
	for _, o := range orgs {
		orgMap[o] = &OrgData{}
	}

	// ── Fetch base data ───────────────────────────────────────────────────────

	header("Scanning Organizations")
	for _, org := range orgs {
		info("Organization: %s%s%s", colorBold, org, colorGreen)

		orgMap[org].Members   = fetchOrgMembers(ctx, client, org, *maxPages)
		orgMap[org].Followers = fetchFollowers(ctx, client, org, *maxPages)
		orgMap[org].Repos     = fetchOrgRepos(ctx, client, org, *maxPages)

		bullet("Members  : %d", len(orgMap[org].Members))
		bullet("Followers: %d", len(orgMap[org].Followers))
		bullet("Repos    : %d", len(orgMap[org].Repos))
	}

	// ── Follower network ──────────────────────────────────────────────────────

	header("Building Follower Network")
	for _, org := range orgs {
		if !*scanFofM && !*scanFofF {
			info("Skipping extended network scan (use -fm and/or -ff to enable)")
			break
		}

		seen := make(map[string]bool)
		for _, u := range orgMap[org].Followers { seen[u] = true }
		for _, u := range orgMap[org].Members   { seen[u] = true }

		var network []string

		if *scanFofM {
			for _, member := range orgMap[org].Members {
				for _, u := range fetchFollowers(ctx, client, member, *maxPages) {
					if !seen[u] { seen[u] = true; network = append(network, u) }
				}
			}
		}

		if *scanFofF {
			for _, follower := range orgMap[org].Followers {
				for _, u := range fetchFollowers(ctx, client, follower, *maxPages) {
					if !seen[u] { seen[u] = true; network = append(network, u) }
				}
			}
		}

		orgMap[org].FollowerNetwork = network
		info("Org %s%s%s — extended network: %d additional users", colorBold, org, colorGreen, len(network))
	}

	// ── Commit history scan ───────────────────────────────────────────────────

	if *scanCommits {
		header("Scanning Commit History")
		for _, org := range orgs {
			known := buildKnownSet(orgMap[org])
			var commitUsers []string
			var unverifiedUsers []string
			seenUnverified := make(map[string]bool)
			for _, repo := range orgMap[org].Repos {
				verified, unverified := fetchRepoCommitUsers(ctx, client, org, repo, *maxPages)
				for _, u := range verified {
					if !known[u] { known[u] = true; commitUsers = append(commitUsers, u) }
				}
				for _, u := range unverified {
					if !seenUnverified[u] { seenUnverified[u] = true; unverifiedUsers = append(unverifiedUsers, u) }
				}
			}
			orgMap[org].CommitUsers     = commitUsers
			orgMap[org].UnverifiedUsers = unverifiedUsers
			info("Org %s%s%s — %d new users found in commit history (%d unverified)", colorBold, org, colorGreen, len(commitUsers), len(unverifiedUsers))
			for _, u := range unverifiedUsers {
				bullet("unverified: %s", u)
			}
		}
	}

	// ── Projects scan ─────────────────────────────────────────────────────────

	if *scanProjects {
		header("Scanning Org Projects")
		for _, org := range orgs {
			known := buildKnownSet(orgMap[org])
			var projectUsers []string
			for _, u := range fetchProjectUsers(ctx, client, org, *pat, *maxPages) {
				if !known[u] { known[u] = true; projectUsers = append(projectUsers, u) }
			}
			orgMap[org].ProjectUsers = projectUsers
			info("Org %s%s%s — %d new users found in projects", colorBold, org, colorGreen, len(projectUsers))
		}
	}

	// ── Issues scan ───────────────────────────────────────────────────────────

	if *scanIssues {
		header("Scanning Repository Issues")
		for _, org := range orgs {
			known := buildKnownSet(orgMap[org])
			var issueUsers []string
			for _, repo := range orgMap[org].Repos {
				for _, u := range fetchIssueUsers(ctx, client, org, repo, *maxPages) {
					if !known[u] { known[u] = true; issueUsers = append(issueUsers, u) }
				}
			}
			orgMap[org].IssueUsers = issueUsers
			info("Org %s%s%s — %d new users found in issues", colorBold, org, colorGreen, len(issueUsers))
		}
	}

	// ── Keyword filtering ─────────────────────────────────────────────────────

	header("Filtering Users by Keywords")
	for _, org := range orgs {
		var keywordUsers []string

		// followers, network, commit, project, issue users are keyword-filtered
		// members are NOT keyword filtered — they go straight to likely_associated
		candidates := []string{}
		candidates = append(candidates, orgMap[org].Followers...)
		candidates = append(candidates, orgMap[org].FollowerNetwork...)
		candidates = append(candidates, orgMap[org].CommitUsers...)
		candidates = append(candidates, orgMap[org].ProjectUsers...)
		candidates = append(candidates, orgMap[org].IssueUsers...)

		for _, u := range candidates {
			if matchesKeywords(ctx, client, u, keywords) {
				keywordUsers = append(keywordUsers, u)
			}
		}

		orgMap[org].KeywordUsers = keywordUsers
		info("Org %s%s%s — %d keyword-matched users", colorBold, org, colorGreen, len(keywordUsers))
	}

	// ── Terminal report ───────────────────────────────────────────────────────

	printReport(orgs, orgMap)

	// ── Determine filenames ───────────────────────────────────────────────────

	prefix := ""
	if *scanName != "" {
		prefix = *scanName + "-"
	}
	fnAllUsers            := prefix + "all_users.txt"
	fnAllUsersMeta        := prefix + "all_users_meta.yaml"
	fnFollowers           := prefix + "org_followers.txt"
	fnNetworkUsers        := prefix + "network_users.txt"
	fnKeywordUsers        := prefix + "keyword_users.txt"
	fnLikelyAssociated   := prefix + "likely_associated_users.txt"
	fnUnverifiedUsers     := prefix + "unverified_users.txt"

	// ── Build output lists ────────────────────────────────────────────────────

	// all_users: every single user scanned
	allUsers := dedupe(collectField(orgMap, func(d *OrgData) []string {
		out := []string{}
		out = append(out, d.Members...)
		out = append(out, d.Followers...)
		out = append(out, d.FollowerNetwork...)
		out = append(out, d.CommitUsers...)
		out = append(out, d.ProjectUsers...)
		out = append(out, d.IssueUsers...)
		out = append(out, d.KeywordUsers...)
		return out
	}))

	// org_followers: direct org followers only
	orgFollowers := dedupe(collectField(orgMap, func(d *OrgData) []string {
		return d.Followers
	}))

	// network_users: followers of followers/members
	networkUsers := dedupe(collectField(orgMap, func(d *OrgData) []string {
		return d.FollowerNetwork
	}))

	// keyword_users: users matched by keyword search
	keywordUsers := dedupe(collectField(orgMap, func(d *OrgData) []string {
		return d.KeywordUsers
	}))

	// likely_associated_users: members + commit + project + issue + keyword users
	likelyAssociated := dedupe(collectField(orgMap, func(d *OrgData) []string {
		out := []string{}
		out = append(out, d.Members...)
		out = append(out, d.CommitUsers...)
		out = append(out, d.ProjectUsers...)
		out = append(out, d.IssueUsers...)
		out = append(out, d.KeywordUsers...)
		return out
	}))

	// unverified: git authors not linked to GitHub accounts
	unverifiedUsers := dedupe(collectField(orgMap, func(d *OrgData) []string {
		return d.UnverifiedUsers
	}))

	// ── Write output files ────────────────────────────────────────────────────

	header("Writing Output Files")

	writeLines(fnAllUsers, allUsers)
	info("Wrote %d unique users → %s", len(allUsers), fnAllUsers)

	writeUserMeta(fnAllUsersMeta, orgs, orgMap)
	info("Wrote user source metadata → %s", fnAllUsersMeta)

	writeLines(fnFollowers, orgFollowers)
	info("Wrote %d org followers → %s", len(orgFollowers), fnFollowers)

	writeLines(fnNetworkUsers, networkUsers)
	info("Wrote %d extended network users → %s", len(networkUsers), fnNetworkUsers)

	writeLines(fnKeywordUsers, keywordUsers)
	info("Wrote %d keyword-matched users → %s", len(keywordUsers), fnKeywordUsers)

	writeLines(fnLikelyAssociated, likelyAssociated)
	info("Wrote %d likely associated users → %s", len(likelyAssociated), fnLikelyAssociated)

	if *scanCommits && len(unverifiedUsers) > 0 {
		writeLines(fnUnverifiedUsers, unverifiedUsers)
		info("Wrote %d unverified git authors → %s", len(unverifiedUsers), fnUnverifiedUsers)
	}
}

// ── Terminal report ───────────────────────────────────────────────────────────

func printReport(orgs []string, orgMap map[string]*OrgData) {
	header("Full Report")

	for _, org := range orgs {
		d := orgMap[org]
		fmt.Printf("\n%s%s%s %s[%s]%s\n", colorBold, colorCyan, org, colorBlue, "organization", colorReset)

		fmt.Printf("  %s%-22s%s (%d)\n", colorBold, "Repositories", colorReset, len(d.Repos))
		for _, r := range d.Repos { bullet("%s", r) }

		fmt.Printf("  %s%-22s%s (%d)\n", colorBold, "Members", colorReset, len(d.Members))
		for _, u := range d.Members { bullet("%s", u) }

		fmt.Printf("  %s%-22s%s (%d — see org_followers.txt)\n", colorBold, "Followers", colorReset, len(d.Followers))

		fmt.Printf("  %s%-22s%s (%d — see network_users.txt)\n", colorBold, "Extended Network", colorReset, len(d.FollowerNetwork))

		if len(d.CommitUsers) > 0 {
			fmt.Printf("  %s%-22s%s (%d — see likely_associated_users.txt)\n", colorBold, "Commit Users", colorReset, len(d.CommitUsers))
		}
		if len(d.ProjectUsers) > 0 {
			fmt.Printf("  %s%-22s%s (%d — see likely_associated_users.txt)\n", colorBold, "Project Users", colorReset, len(d.ProjectUsers))
		}
		if len(d.IssueUsers) > 0 {
			fmt.Printf("  %s%-22s%s (%d — see likely_associated_users.txt)\n", colorBold, "Issue Users", colorReset, len(d.IssueUsers))
		}

		fmt.Printf("  %s%-22s%s (%d)\n", colorBold, "Keyword Users", colorReset, len(d.KeywordUsers))
		for _, u := range d.KeywordUsers {
			bullet("%s%s%s", colorGreen, u, colorReset)
		}
	}
}

// ── YAML metadata writer ──────────────────────────────────────────────────────

func writeUserMeta(path string, orgs []string, orgMap map[string]*OrgData) {
	f, err := os.Create(path)
	fatalIf("creating "+path, err)
	defer f.Close()

	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "# User source metadata")
	fmt.Fprintln(w, "# Organised by organization, then by category")

	for _, org := range orgs {
		d := orgMap[org]
		fmt.Fprintf(w, "\n%s:\n", org)

		writeYAMLCategory(w, "members", d.Members)
		writeYAMLCategory(w, "followers", d.Followers)
		writeYAMLCategory(w, "follower_network", d.FollowerNetwork)
		writeYAMLCategory(w, "commit_history", d.CommitUsers)
		writeYAMLCategory(w, "projects", d.ProjectUsers)
		writeYAMLCategory(w, "issues", d.IssueUsers)
		writeYAMLCategory(w, "keyword_matches", d.KeywordUsers)
		writeYAMLCategory(w, "unverified_git_authors", d.UnverifiedUsers)
	}

	fatalIf("flushing "+path, w.Flush())
}

func writeYAMLCategory(w *bufio.Writer, category string, users []string) {
	if len(users) == 0 {
		return
	}
	sorted := make([]string, len(users))
	copy(sorted, users)
	slices.Sort(sorted)
	fmt.Fprintf(w, "  %s:\n", category)
	for _, u := range sorted {
		fmt.Fprintf(w, "    - %q\n", u)
	}
}

// ── Rate limit helpers ────────────────────────────────────────────────────────

func checkRateLimit(resp *github.Response, label string) bool {
	if resp == nil {
		return false
	}
	rl := resp.Rate
	if rl.Remaining == 0 {
		fmt.Printf("\n%s%s⚠ RATE LIMIT REACHED%s — %s\n", colorBold, colorRed, colorReset, label)
		fmt.Printf("  Limit    : %d\n", rl.Limit)
		fmt.Printf("  Remaining: %s0%s\n", colorRed, colorReset)
		fmt.Printf("  Resets at: %s\n\n", rl.Reset.Time.Local().Format("15:04:05 MST"))
		return true
	}
	if rl.Remaining < 50 {
		fmt.Printf("  %s⚠ Rate limit low%s — %d / %d remaining (resets %s)\n",
			colorYellow, colorReset, rl.Remaining, rl.Limit,
			rl.Reset.Time.Local().Format("15:04:05 MST"))
	}
	return false
}

func isRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*github.RateLimitError); ok {
		return true
	}
	return strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "429")
}

func handleAPIErr(label string, err error, resp *github.Response) bool {
	if err == nil {
		return false
	}
	if isRateLimitErr(err) {
		reset := "unknown"
		if resp != nil {
			reset = resp.Rate.Reset.Time.Local().Format("15:04:05 MST")
		}
		fmt.Printf("\n%s%s⚠ THROTTLED / RATE LIMITED%s — %s\n", colorBold, colorRed, colorReset, label)
		fmt.Printf("  GitHub has temporarily blocked requests. Resets at: %s\n\n", reset)
	} else {
		warn("API error (%s): %v", label, err)
	}
	return true
}

// ── GitHub helpers ────────────────────────────────────────────────────────────

func fetchOrgMembers(ctx context.Context, client *github.Client, org string, maxPages int) []string {
	opts := &github.ListMembersOptions{
		PublicOnly:  true,
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var names []string
	page := 0
	for {
		members, resp, err := client.Organizations.ListMembers(ctx, org, opts)
		if handleAPIErr("ListMembers:"+org, err, resp) { break }
		checkRateLimit(resp, "ListMembers:"+org)
		for _, m := range members { names = append(names, m.GetLogin()) }
		page++
		if resp.NextPage == 0 || (maxPages > 0 && page >= maxPages) { break }
		opts.Page = resp.NextPage
	}
	return names
}

func fetchFollowers(ctx context.Context, client *github.Client, user string, maxPages int) []string {
	opts := &github.ListOptions{PerPage: 100}
	var names []string
	page := 0
	for {
		followers, resp, err := client.Users.ListFollowers(ctx, user, opts)
		if handleAPIErr("ListFollowers:"+user, err, resp) { break }
		checkRateLimit(resp, "ListFollowers:"+user)
		for _, f := range followers { names = append(names, f.GetLogin()) }
		page++
		if resp.NextPage == 0 || (maxPages > 0 && page >= maxPages) { break }
		opts.Page = resp.NextPage
	}
	return names
}

func fetchOrgRepos(ctx context.Context, client *github.Client, org string, maxPages int) []string {
	opts := &github.RepositoryListByOrgOptions{
		Type:        "all",
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var names []string
	page := 0
	for {
		repos, resp, err := client.Repositories.ListByOrg(ctx, org, opts)
		if handleAPIErr("ListByOrg:"+org, err, resp) { break }
		checkRateLimit(resp, "ListByOrg:"+org)
		for _, r := range repos { names = append(names, r.GetName()) }
		page++
		if resp.NextPage == 0 || (maxPages > 0 && page >= maxPages) { break }
		opts.Page = resp.NextPage
	}
	return names
}

// buildKnownSet returns a set of all users discovered so far for an org.
func buildKnownSet(d *OrgData) map[string]bool {
	known := make(map[string]bool)
	for _, u := range d.Members         { known[u] = true }
	for _, u := range d.Followers       { known[u] = true }
	for _, u := range d.FollowerNetwork { known[u] = true }
	for _, u := range d.CommitUsers     { known[u] = true }
	for _, u := range d.ProjectUsers    { known[u] = true }
	for _, u := range d.IssueUsers      { known[u] = true }
	return known
}

func fetchProjectUsers(ctx context.Context, client *github.Client, org, pat string, maxPages int) []string {
	seen := make(map[string]bool)
	var names []string
	addUser := func(u string) {
		if u != "" && !seen[u] { seen[u] = true; names = append(names, u) }
	}
	hasV1 := fetchProjectUsersV1(ctx, client, org, maxPages, addUser)
	hasV2 := fetchProjectUsersV2(ctx, pat, org, maxPages, addUser)
	if !hasV1 && !hasV2 {
		warn("No projects found for org %s (checked both v1 classic and v2)", org)
	}
	return names
}

func fetchProjectUsersV1(ctx context.Context, client *github.Client, org string, maxPages int, addUser func(string)) bool {
	projOpts := &github.ProjectListOptions{ListOptions: github.ListOptions{PerPage: 100}}
	found := false
	page := 0
	for {
		projects, resp, err := client.Organizations.ListProjects(ctx, org, projOpts)
		if err != nil {
			if resp != nil && (resp.StatusCode == 404 || resp.StatusCode == 410) { break }
			handleAPIErr("ListProjectsV1:"+org, err, resp)
			break
		}
		checkRateLimit(resp, "ListProjectsV1:"+org)
		if len(projects) > 0 && !found {
			found = true
			info("Detected Projects v1 (classic) for org %s", org)
		}
		for _, proj := range projects {
			if proj.Creator != nil { addUser(proj.Creator.GetLogin()) }
			colOpts := &github.ListOptions{PerPage: 100}
			for {
				cols, colResp, colErr := client.Projects.ListProjectColumns(ctx, proj.GetID(), colOpts)
				if handleAPIErr(fmt.Sprintf("ListProjectColumns:%d", proj.GetID()), colErr, colResp) { break }
				checkRateLimit(colResp, fmt.Sprintf("ListProjectColumns:%d", proj.GetID()))
				for _, col := range cols {
					cardOpts := &github.ProjectCardListOptions{ListOptions: github.ListOptions{PerPage: 100}}
					for {
						cards, cardResp, cardErr := client.Projects.ListProjectCards(ctx, col.GetID(), cardOpts)
						if handleAPIErr(fmt.Sprintf("ListProjectCards:%d", col.GetID()), cardErr, cardResp) { break }
						checkRateLimit(cardResp, fmt.Sprintf("ListProjectCards:%d", col.GetID()))
						for _, card := range cards {
							if card.Creator != nil { addUser(card.Creator.GetLogin()) }
						}
						if cardResp.NextPage == 0 { break }
						cardOpts.Page = cardResp.NextPage
					}
				}
				if colResp.NextPage == 0 { break }
				colOpts.Page = colResp.NextPage
			}
		}
		page++
		if resp.NextPage == 0 || (maxPages > 0 && page >= maxPages) { break }
		projOpts.Page = resp.NextPage
	}
	return found
}

func fetchProjectUsersV2(ctx context.Context, pat, org string, maxPages int, addUser func(string)) bool {
	if pat == "" {
		warn("Skipping Projects v2 scan — a PAT with 'read:project' scope is required (-t flag)")
		return false
	}

	const query = `
	query($org: String!, $projCursor: String, $itemCursor: String) {
		organization(login: $org) {
			projectsV2(first: 20, after: $projCursor) {
				pageInfo { hasNextPage endCursor }
				nodes {
					title
					creator { login }
					items(first: 100, after: $itemCursor) {
						pageInfo { hasNextPage endCursor }
						nodes {
							creator { login }
							... on ProjectV2Item {
								fieldValues(first: 10) {
									nodes {
										... on ProjectV2ItemFieldUserValue {
											users(first: 10) {
												nodes { login }
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}`

	type pageInfo    struct { HasNextPage bool; EndCursor string }
	type userNode    struct { Login string `json:"login"` }
	type userConn    struct { Nodes []userNode `json:"nodes"` }
	type fieldValue  struct { Users userConn `json:"users"` }
	type fieldValues struct { Nodes []fieldValue `json:"nodes"` }
	type itemNode    struct {
		Creator     *userNode   `json:"creator"`
		FieldValues fieldValues `json:"fieldValues"`
	}
	type itemConn    struct { PageInfo pageInfo `json:"pageInfo"`; Nodes []itemNode `json:"nodes"` }
	type projectNode struct {
		Title   string    `json:"title"`
		Creator *userNode `json:"creator"`
		Items   itemConn  `json:"items"`
	}
	type projectConn struct { PageInfo pageInfo `json:"pageInfo"`; Nodes []*projectNode `json:"nodes"` }
	type orgData     struct { ProjectsV2 projectConn `json:"projectsV2"` }
	type gqlData     struct { Organization orgData `json:"organization"` }
	type gqlResp     struct {
		Data   gqlData `json:"data"`
		Errors []struct{ Message string `json:"message"` } `json:"errors"`
	}

	doQuery := func(projCursor, itemCursor string) (*gqlResp, error) {
		vars := map[string]any{"org": org}
		if projCursor != "" { vars["projCursor"] = projCursor }
		if itemCursor != "" { vars["itemCursor"] = itemCursor }
		body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(body))
		if err != nil { return nil, err }
		req.Header.Set("Authorization", "Bearer "+pat)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil { return nil, err }
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("rate limited (HTTP %d)", resp.StatusCode)
		}
		rawBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil { return nil, fmt.Errorf("failed to read response body: %w", readErr) }
		var result gqlResp
		if err := json.Unmarshal(rawBody, &result); err != nil { return nil, err }
		if len(result.Errors) > 0 { return nil, fmt.Errorf("GraphQL error: %s", result.Errors[0].Message) }
		return &result, nil
	}

	found := false
	projCursor := ""
	projPage := 0
	for {
		result, err := doQuery(projCursor, "")
		if err != nil { warn("Projects v2 GraphQL query failed for %s: %v", org, err); break }

		projects := result.Data.Organization.ProjectsV2
		if len(projects.Nodes) > 0 && !found {
			found = true
			info("Detected Projects v2 for org %s", org)
		}
		for _, proj := range projects.Nodes {
			if proj == nil {
				warn("Projects v2: received null project node for org %s — check that your PAT has 'read:project' scope", org)
				continue
			}
			if proj.Creator != nil { addUser(proj.Creator.Login) }
			itemCursor := ""
			for {
				var itemResult *gqlResp
				var itemErr error
				if itemCursor == "" {
					itemResult = result
				} else {
					itemResult, itemErr = doQuery(projCursor, itemCursor)
					if itemErr != nil { warn("Projects v2 item pagination failed: %v", itemErr); break }
					for _, p := range itemResult.Data.Organization.ProjectsV2.Nodes {
						if p != nil && p.Title == proj.Title { proj.Items = p.Items; break }
					}
				}
				for _, item := range proj.Items.Nodes {
					if item.Creator != nil { addUser(item.Creator.Login) }
					for _, fv := range item.FieldValues.Nodes {
						for _, u := range fv.Users.Nodes { addUser(u.Login) }
					}
				}
				if !proj.Items.PageInfo.HasNextPage { break }
				itemCursor = proj.Items.PageInfo.EndCursor
			}
		}
		projPage++
		if !projects.PageInfo.HasNextPage || (maxPages > 0 && projPage >= maxPages) { break }
		projCursor = projects.PageInfo.EndCursor
	}
	return found
}

func fetchIssueUsers(ctx context.Context, client *github.Client, org, repo string, maxPages int) []string {
	opts := &github.IssueListByRepoOptions{
		State:       "all",
		ListOptions: github.ListOptions{PerPage: 100},
	}
	seen := make(map[string]bool)
	var names []string
	page := 0
	for {
		issues, resp, err := client.Issues.ListByRepo(ctx, org, repo, opts)
		if handleAPIErr("ListIssues:"+org+"/"+repo, err, resp) { break }
		checkRateLimit(resp, "ListIssues:"+org+"/"+repo)
		for _, issue := range issues {
			if u := issue.GetUser().GetLogin(); u != "" && !seen[u] { seen[u] = true; names = append(names, u) }
			for _, a := range issue.Assignees {
				if u := a.GetLogin(); u != "" && !seen[u] { seen[u] = true; names = append(names, u) }
			}
		}
		page++
		if resp.NextPage == 0 || (maxPages > 0 && page >= maxPages) { break }
		opts.Page = resp.NextPage
	}
	return names
}

func fetchRepoCommitUsers(ctx context.Context, client *github.Client, org, repo string, maxPages int) (verified []string, unverified []string) {
	opts := &github.CommitsListOptions{ListOptions: github.ListOptions{PerPage: 100}}
	seenV := make(map[string]bool)
	seenU := make(map[string]bool)
	page := 0
	for {
		commits, resp, err := client.Repositories.ListCommits(ctx, org, repo, opts)
		if handleAPIErr("ListCommits:"+org+"/"+repo, err, resp) { break }
		checkRateLimit(resp, "ListCommits:"+org+"/"+repo)
		for _, c := range commits {
			if c.Author != nil {
				if login := c.Author.GetLogin(); login != "" && !seenV[login] {
					seenV[login] = true
					verified = append(verified, login)
					continue
				}
			}
			if c.Commit != nil && c.Commit.Author != nil {
				if name := c.Commit.Author.GetName(); name != "" && !seenU[name] {
					seenU[name] = true
					unverified = append(unverified, name)
				}
			}
		}
		page++
		if resp.NextPage == 0 || (maxPages > 0 && page >= maxPages) { break }
		opts.Page = resp.NextPage
	}
	return verified, unverified
}

func matchesKeywords(ctx context.Context, client *github.Client, login string, keywords []string) bool {
	if len(keywords) == 0 { return false }
	u, resp, err := client.Users.Get(ctx, login)
	if handleAPIErr("Users.Get:"+login, err, resp) { return false }
	if u == nil { return false }
	checkRateLimit(resp, "Users.Get:"+login)

	normalize := func(s string) string {
		s = strings.ToLower(s)
		s = strings.ReplaceAll(s, " ", "")
		s = strings.ReplaceAll(s, ".", "")
		return s
	}
	company := normalize(u.GetCompany())
	bio     := normalize(u.GetBio())
	for _, kw := range keywords {
		kw = normalize(strings.TrimSpace(kw))
		if kw == "" { continue }
		if strings.Contains(company, kw) || strings.Contains(bio, kw) {
			fmt.Printf("    %s✓ Match%s — %s (company: %q | bio: %q)\n",
				colorGreen, colorReset, login, u.GetCompany(), u.GetBio())
			return true
		}
	}
	return false
}

// ── File I/O helpers ──────────────────────────────────────────────────────────

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil { return nil, err }
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" { continue }
		line = strings.TrimPrefix(line, "https://github.com/")
		line = strings.TrimPrefix(line, "http://github.com/")
		line = strings.TrimRight(line, "/")
		lines = append(lines, line)
	}
	return lines, sc.Err()
}

func writeLines(path string, lines []string) {
	f, err := os.Create(path)
	fatalIf("creating "+path, err)
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, l := range lines { fmt.Fprintln(w, l) }
	fatalIf("flushing "+path, w.Flush())
}

// ── Utility ───────────────────────────────────────────────────────────────────

func dedupe(s []string) []string {
	seen := make(map[string]bool, len(s))
	out  := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] { seen[v] = true; out = append(out, v) }
	}
	return out
}

func collectField(m map[string]*OrgData, fn func(*OrgData) []string) []string {
	var out []string
	for _, d := range m { out = append(out, fn(d)...) }
	return out
}


func fatalIf(ctx string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sFatal (%s): %v%s\n", colorRed, ctx, err, colorReset)
		os.Exit(1)
	}
}