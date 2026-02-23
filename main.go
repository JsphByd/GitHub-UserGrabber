package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
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
	FilteredUsers   []string // members + keyword-matched followers
	FollowerNetwork []string // followers-of-members and followers-of-followers
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	orgsFile := flag.String("o", "", "Path to file containing GitHub organization names (one per line)")
	kwFile := flag.String("k", "", "Path to file containing keywords to filter users (one per line)")
	pat := flag.String("t", "", "GitHub Personal Access Token (required to avoid rate limiting)")
	scanFofM := flag.Bool("fm", false, "Also scan followers of org members")
	scanFofF := flag.Bool("ff", false, "Also scan followers of org followers")
	maxPages := flag.Int("p", 0, "Max number of pages to fetch per API call (0 = unlimited)")
	flag.Parse()

	if *orgsFile == "" || *kwFile == "" {
		fmt.Fprintf(os.Stderr, "%sUsage: github-scanner -o <orgs_file> -k <keywords_file> [-t <github_pat>] [-fm] [-ff] [-p <max_pages>]%s\n", colorRed, colorReset)
		flag.PrintDefaults()
		os.Exit(1)
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

		orgMap[org].Members = fetchOrgMembers(ctx, client, org, *maxPages)
		orgMap[org].Followers = fetchFollowers(ctx, client, org, *maxPages)
		orgMap[org].Repos = fetchOrgRepos(ctx, client, org, *maxPages)

		bullet("Members  : %d", len(orgMap[org].Members))
		bullet("Followers: %d", len(orgMap[org].Followers))
		bullet("Repos    : %d", len(orgMap[org].Repos))
	}

	// ── Follower network (followers-of-members + followers-of-followers) ──────

	header("Building Follower Network")
	for _, org := range orgs {
		if !*scanFofM && !*scanFofF {
			info("Skipping extended network scan (use -fm and/or -ff to enable)")
			break
		}

		seen := make(map[string]bool)
		for _, u := range orgMap[org].Followers {
			seen[u] = true
		}
		for _, u := range orgMap[org].Members {
			seen[u] = true
		}

		var network []string

		if *scanFofM {
			for _, member := range orgMap[org].Members {
				for _, u := range fetchFollowers(ctx, client, member, *maxPages) {
					if !seen[u] {
						seen[u] = true
						network = append(network, u)
					}
				}
			}
		}

		if *scanFofF {
			for _, follower := range orgMap[org].Followers {
				for _, u := range fetchFollowers(ctx, client, follower, *maxPages) {
					if !seen[u] {
						seen[u] = true
						network = append(network, u)
					}
				}
			}
		}

		orgMap[org].FollowerNetwork = network
		info("Org %s%s%s — extended network: %d additional users", colorBold, org, colorGreen, len(network))
	}

	// ── Keyword filtering ─────────────────────────────────────────────────────

	header("Filtering Users by Keywords")
	for _, org := range orgs {
		var filtered []string

		// All members are always included in the filtered output
		filtered = append(filtered, orgMap[org].Members...)

		// Filter direct followers
		for _, u := range orgMap[org].Followers {
			if matchesKeywords(ctx, client, u, keywords) {
				filtered = append(filtered, u)
			}
		}

		// Filter follower-network users
		for _, u := range orgMap[org].FollowerNetwork {
			if matchesKeywords(ctx, client, u, keywords) {
				filtered = append(filtered, u)
			}
		}

		orgMap[org].FilteredUsers = filtered
		info("Org %s%s%s — %d filtered users", colorBold, org, colorGreen, len(filtered))
	}

	// ── Pretty terminal report ────────────────────────────────────────────────

	printReport(orgs, orgMap)

	// ── Write output files ────────────────────────────────────────────────────

	header("Writing Output Files")

	allUsers := dedupe(collectField(orgMap, func(d *OrgData) []string {
		combined := d.Members
		combined = append(combined, d.Followers...)
		combined = append(combined, d.FollowerNetwork...)
		return combined
	}))

	filteredUsers := dedupe(collectField(orgMap, func(d *OrgData) []string {
		return d.FilteredUsers
	}))

	writeLines("all_users.txt", allUsers)
	info("Wrote %d unique users → all_users.txt", len(allUsers))

	writeLines("filtered_users.txt", filteredUsers)
	info("Wrote %d unique filtered users → filtered_users.txt", len(filteredUsers))
}

// ── Terminal report ───────────────────────────────────────────────────────────

func printReport(orgs []string, orgMap map[string]*OrgData) {
	header("Full Report")

	for _, org := range orgs {
		d := orgMap[org]
		fmt.Printf("\n%s%s%s %s[%s]%s\n", colorBold, colorCyan, org, colorBlue, "organization", colorReset)

		fmt.Printf("  %s%-18s%s (%d)\n", colorBold, "Repositories", colorReset, len(d.Repos))
		for _, r := range d.Repos {
			bullet("%s", r)
		}

		fmt.Printf("  %s%-18s%s (%d)\n", colorBold, "Members", colorReset, len(d.Members))
		for _, u := range d.Members {
			bullet("%s", u)
		}

		fmt.Printf("  %s%-18s%s (%d)\n", colorBold, "Followers", colorReset, len(d.Followers))
		for _, u := range d.Followers {
			bullet("%s", u)
		}

		fmt.Printf("  %s%-18s%s (%d — see all_users.txt)\n", colorBold, "Extended Network", colorReset, len(d.FollowerNetwork))

		// Only show extended-network users that made it into the filtered list
		memberSet := make(map[string]bool, len(d.Members))
		for _, u := range d.Members {
			memberSet[u] = true
		}
		followerSet := make(map[string]bool, len(d.Followers))
		for _, u := range d.Followers {
			followerSet[u] = true
		}
		fmt.Printf("  %s%-18s%s (%d)\n", colorBold, "Filtered Users", colorReset, len(d.FilteredUsers))
		for _, u := range d.FilteredUsers {
			source := "member"
			if !memberSet[u] && followerSet[u] {
				source = "direct follower"
			} else if !memberSet[u] && !followerSet[u] {
				source = "extended network"
			}
			bullet("%s%s%s %s(%s)%s", colorGreen, u, colorReset, colorYellow, source, colorReset)
		}
	}
}

// ── GitHub helpers ────────────────────────────────────────────────────────────

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
	_, ok := err.(*github.RateLimitError)
	if ok {
		return true
	}
	if strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "429") {
		return true
	}
	return false
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
		if handleAPIErr("ListMembers:"+org, err, resp) {
			break
		}
		checkRateLimit(resp, "ListMembers:"+org)
		for _, m := range members {
			names = append(names, m.GetLogin())
		}
		page++
		if resp.NextPage == 0 || (maxPages > 0 && page >= maxPages) {
			break
		}
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
		if handleAPIErr("ListFollowers:"+user, err, resp) {
			break
		}
		checkRateLimit(resp, "ListFollowers:"+user)
		for _, f := range followers {
			names = append(names, f.GetLogin())
		}
		page++
		if resp.NextPage == 0 || (maxPages > 0 && page >= maxPages) {
			break
		}
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
		if handleAPIErr("ListByOrg:"+org, err, resp) {
			break
		}
		checkRateLimit(resp, "ListByOrg:"+org)
		for _, r := range repos {
			names = append(names, r.GetName())
		}
		page++
		if resp.NextPage == 0 || (maxPages > 0 && page >= maxPages) {
			break
		}
		opts.Page = resp.NextPage
	}
	return names
}

func matchesKeywords(ctx context.Context, client *github.Client, login string, keywords []string) bool {
	u, resp, err := client.Users.Get(ctx, login)
	if handleAPIErr("Users.Get:"+login, err, resp) {
		return false
	}
	if u == nil {
		return false
	}
	checkRateLimit(resp, "Users.Get:"+login)

	normalize := func(s string) string {
		s = strings.ToLower(s)
		s = strings.ReplaceAll(s, " ", "")
		s = strings.ReplaceAll(s, ".", "")
		return s
	}

	company := normalize(u.GetCompany())
	bio := normalize(u.GetBio())

	for _, kw := range keywords {
		kw = normalize(strings.TrimSpace(kw))
		if kw == "" {
			continue
		}
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
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, sc.Err()
}

func writeLines(path string, lines []string) {
	f, err := os.Create(path)
	fatalIf("creating "+path, err)
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
	fatalIf("flushing "+path, w.Flush())
}

// ── Utility ───────────────────────────────────────────────────────────────────

func dedupe(s []string) []string {
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func collectField(m map[string]*OrgData, fn func(*OrgData) []string) []string {
	var out []string
	for _, d := range m {
		out = append(out, fn(d)...)
	}
	return out
}

func fatalIf(ctx string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sFatal (%s): %v%s\n", colorRed, ctx, err, colorReset)
		os.Exit(1)
	}
}
