package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/github"
)

func main() {
	client := github.NewClient(nil)
	orgToFollowersMap, _ := read_file_return_list("C:\\Users\\JosephBoyd\\Documents\\GitHub-Scanner\\organizations.txt") //get org list
	get_GitHub_Org_Members(orgToFollowersMap, client)
}

func read_file_return_list(path string) (map[string][]string, error) { //get the orgs from a file. return a map of org:string[]
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string][]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		result[line] = append(result[line], "")
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func print_files() { //will need to build some sort of struct to store data?
	fmt.Println("Print out the files and data")
}

func check_user() { //keywords hashmap, user
	fmt.Println("Check user for keywords in bio")
}

func get_GitHub_Org_Members(orgToFollowersMap map[string][]string, client *github.Client) map[string][]string { //organization or user
	ctx := context.Background()

	opts := &github.ListMembersOptions{
		PublicOnly:  true,
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var followers []*github.User
	var usernames []string
	for org := range orgToFollowersMap {
		followers, _, _ = client.Organizations.ListMembers(ctx, org, opts)

		for user := range followers {
			usernames = append(usernames, *followers[user].Login)
		}
		orgToFollowersMap[org] = usernames
	}

	fmt.Print(orgToFollowersMap)

	return orgToFollowersMap
}

func get_stats() { //organization or user
	fmt.Println("get the number of followers from a user or organization")
}

func build_output() { //will need to build some sort of struct to store data?
	fmt.Println("build output files")
}
