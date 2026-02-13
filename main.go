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

	filePath := "C:\\Users\\JosephBoyd\\Documents\\GitHub-Scanner\\organizations.txt"

	orgArray, _ = read_file_return_list(filePath)
	keywords, _ = read_file_return_list(filePath)

	orgToMembersMap := make(map[string][]string)
	for org := range orgArray {
		index := orgArray[org]
		orgToMembersMap[index] = append(orgToMembersMap[index], "")
	}

	orgToFollowersMap := maps.Clone(orgToMembersMap)
	orgToFollowersMapFiltered := maps.Clone(orgToMembersMap)
	orgToReposMap := maps.Clone(orgToMembersMap)

	orgToReposMap = get_org_repositories(orgToReposMap, client)
	orgToMembersMap, orgToFollowersMap = get_GitHub_Org_Members(orgToMembersMap, orgToFollowersMap, client)
	orgToFollowersMapFiltered = filter_followers(orgToFollowersMapFiltered, orgToFollowersMap, keywords, client)

	fmt.Println(orgToFollowersMapFiltered, orgToFollowersMapFiltered, orgToReposMap)
}

func filter_followers(orgToFollowersMapFiltered map[string][]string, orgToFollowersMap map[string][]string, keywords []string, client *github.Client) map[string][]string {
	ctx := context.Background()
	//var finds []string

	for org := range orgToFollowersMap {
		followersArray := orgToFollowersMap[org]
		for follower := range orgToFollowersMap[org] {
			//get company and bio
			follower_data, _, _ := client.Users.Get(ctx, followersArray[follower])
			if follower_data.Bio != nil {
				//search functionality here.
			}
			if follower_data.Company != nil {
				if _, ok := orgToFollowersMap[*follower_data.Company]; ok {
					orgToFollowersMapFiltered[org] = append(orgToFollowersMapFiltered[org], *follower_data.Login)
				}
			}
		}
	}
	return orgToFollowersMapFiltered
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

func print_files() { //will need to build some sort of struct to store data?
	fmt.Println("Print out the files and data")
	//all users
	//organization members
	//organization followers
	//organization followers filtered
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
