# GitHub Organization Crawler

Collect all members, users, and repositories belonging to a GitHub organization for auditing or secrets scanning.

# Build

This repository comes with a pre-compiled executable/binary. However, you can rebuild using:

```
go build .
```

# Run
Default: scan without a PAT token. Limited to 60 requests per hour.
```
github-scanner -o orgs.txt -k keywords.txt
```

Provide a PAT token to make 5,000 requests per hour.
```
github-scanner -o orgs.txt -k keywords.txt -t <PAT TOKEN>
```

Requests can be limited by page. The following example only fetches the first page of results of an organization's members/followers/repositories. This can be used to limit the number of requests to the API.
```
github-scanner -o orgs.txt -k keywords.txt -p 1
```

# Args
```
Usage: github-scanner -o <orgs_file> -k <keywords_file> [-t <github_pat>] [-fm] [-ff] [-p <max_pages>]
  -ff
        Also scan followers of org followers
  -fm
        Also scan followers of org members
  -k string
        Path to file containing keywords to filter users (one per line)
  -o string
        Path to file containing GitHub organization names (one per line)
  -p int
        Max number of pages to fetch per API call (0 = unlimited)
  -t string
        GitHub Personal Access Token (required to avoid rate limiting)
```
