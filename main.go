package main

import (
	"flag"
	"fmt"
)

type orgList []string

func main() {

	var orgArgs orgList

	flag.Var(&orgArgs, "list1", "Some description for this param.")

	flag.Parse()
	fmt.Println("word:", *wordPtr)

}

func read_file_return_list() { //pass: filepath
	fmt.Println("read from files here")
}

func print_files() { //will need to build some sort of struct to store data?
	fmt.Println("Print out the files and data")
}

func check_user() { //keywords hashmap, user
	fmt.Println("Check user for keywords in bio")
}

func get__GitHub_followers() { //organization or user
	fmt.Println("get github followers from a user or organization")
}

func get_stats() { //organization or user
	fmt.Println("get the number of followers from a user or organization")
}

func build_output() { //will need to build some sort of struct to store data?
	fmt.Println("build output files")
}
