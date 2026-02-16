package main

import (
	"bufio"
	"context"
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/google/go-github/github"
)

func main() {
	client := github.NewClient(nil)

	var orgArray []string
	var keywords []string

	keywords = append(keywords, "Powered by Citizen")
	keywords = append(keywords, "U.S Chamber of Commerce")

	filePath := "C:\\Users\\JosephBoyd\\Documents\\GitHub-Scanner\\organizations.txt"

	orgArray, _ = read_file_return_list(filePath)
	//keywords, _ = read_file_return_list(filePath)

	orgToMembersMap := make(map[string][]string)
	for org := range orgArray {
		index := orgArray[org]
		orgToMembersMap[index] = append(orgToMembersMap[index], "")
	}

	//clone new maps using the original one
	orgToFollowersMap := maps.Clone(orgToMembersMap)
	orgToFollowersMapFiltered := maps.Clone(orgToMembersMap)
	orgToReposMap := maps.Clone(orgToMembersMap)

	//populate the three maps
	orgToReposMap = get_org_repositories(orgToReposMap, client)
	orgToMembersMap, orgToFollowersMap = get_GitHub_Org_Members(orgToMembersMap, orgToFollowersMap, client)
	orgToFollowersMapFiltered = filter_followers(orgToFollowersMapFiltered, orgToFollowersMap, keywords, client)

	fmt.Println(orgToFollowersMapFiltered, orgToFollowersMapFiltered, orgToReposMap)

	//print out all users found
	var allUsersArray []string
	var filteredUsersArray []string
	for _, users := range orgToFollowersMap {
		allUsersArray = append(allUsersArray, users...)
	}
	for _, users := range orgToFollowersMapFiltered {
		filteredUsersArray = append(filteredUsersArray, users...)
	}

	print_files("Test-allUsers.txt", allUsersArray)
	print_files("Test-filteredUsers.txt", filteredUsersArray)
}

func filter_followers(orgToFollowersMapFiltered map[string][]string, orgToFollowersMap map[string][]string, keywords []string, client *github.Client) map[string][]string {
	ctx := context.Background()
	//var finds []string

	for org := range orgToFollowersMap {
		followersArray := orgToFollowersMap[org]
		for follower := range orgToFollowersMap[org] {
			//get company and bio
			follower_data, _, _ := client.Users.Get(ctx, followersArray[follower])
			user := search_and_match(follower_data, keywords)
			if user != "NO RESULTS" {
				orgToFollowersMapFiltered[org] = append(orgToFollowersMapFiltered[org], user)
			}
		}
	}
	return orgToFollowersMapFiltered
}

func search_and_match(userData *github.User, keywords []string) string {
	var lowerCompany string
	var lowerBio string
	if userData == nil || len(keywords) == 0 {
		fmt.Println("EMPTY KEYWORD LIST")
		return "NO RESULTS"
	}

	lowerKeywords := make([]string, 0, len(keywords))
	for _, k := range keywords {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		k = strings.ReplaceAll(k, " ", "")
		k = strings.ReplaceAll(k, ".", "")
		lowerKeywords = append(lowerKeywords, strings.ToLower(k))
	}

	if userData.Company != nil {
		lowerCompany = strings.ToLower(*userData.Company)
		lowerCompany = strings.ReplaceAll(lowerCompany, " ", "")
		lowerCompany = strings.ReplaceAll(lowerCompany, ".", "")
	} else {
		lowerCompany = ""
	}

	if userData.Bio != nil {
		lowerBio = strings.ToLower(*userData.Bio)
	} else {
		lowerBio = ""
	}
	for keyword := range lowerKeywords {
		if lowerKeywords[keyword] == lowerCompany {
			fmt.Println("found match for", *userData.Company)
			return *userData.Login
		} else if lowerKeywords[keyword] == lowerBio {
			fmt.Println("found match for", *userData.Bio) //WIP - need regex
		}
	}
	return "NO RESULTS"
}

func get_org_repositories(orgToReposMap map[string][]string, client *github.Client) map[string][]string {
	ctx := context.Background()
	var repositories []*github.Repository
	var repository_names []string

	repoListOpts := &github.RepositoryListByOrgOptions{
		// Type of repositories to list. Possible values are: all, public, private,
		// forks, sources, member. Default is "all".
		Type: "all",
	}

	for org := range orgToReposMap {
		repositories, _, _ = client.Repositories.ListByOrg(ctx, org, repoListOpts)
		for repo := range repositories {
			repository_names = append(repository_names, *repositories[repo].Name)
		}
		orgToReposMap[org] = repository_names
	}

	return orgToReposMap
}

func read_file_return_list(path string) ([]string, error) { //get the orgs from a file. return a map of org:string[]
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var result []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		result = append(result, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, err
}

func print_files(filename string, contentArray []string) { //will need to build some sort of struct to store data?
	fmt.Println("Print out the files and data")
	//all users
	//organization members
	//organization followers
	//organization followers filtered"

	content := ""

	for user := range contentArray {
		content = content + contentArray[user] + "\n"
	}

	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write file: %v\n", err)
		os.Exit(1)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(data))
}

func filter_users() { //keywords hashmap, user
	fmt.Println("Check user for keywords in bio")
}

func get_GitHub_Org_Members(orgToMembersMap map[string][]string, orgToFollowersMap map[string][]string, client *github.Client) (map[string][]string, map[string][]string) { //organization or user
	ctx := context.Background()
	var members []*github.User
	var followers []*github.User
	var memberUsernames []string
	var followerUsernames []string

	opts := &github.ListMembersOptions{
		PublicOnly:  true,
		ListOptions: github.ListOptions{PerPage: 100},
	}

	opts1 := &github.ListOptions{
		PerPage: 100,
	}

	for org := range orgToMembersMap {
		members, _, _ = client.Organizations.ListMembers(ctx, org, opts) //members
		followers, _, _ = client.Users.ListFollowers(ctx, org, opts1)    //followers
		for user := range members {
			memberUsernames = append(memberUsernames, *members[user].Login)
		}
		for user := range followers {
			followerUsernames = append(followerUsernames, *followers[user].Login)
		}
		orgToMembersMap[org] = memberUsernames
		orgToFollowersMap[org] = followerUsernames
	}
	return orgToMembersMap, orgToFollowersMap
}

func get_stats() { //organization or user
	fmt.Println("get the number of followers from a user or organization")
}

func build_output() { //will need to build some sort of struct to store data?
	fmt.Println("build output files")
}
