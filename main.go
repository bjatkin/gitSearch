package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
)

type ConfigSettings struct {
	Port  string   `yaml:"port"`
	Repos []string `yaml:"repos"`
}

// Global Configuration for the service
var Config ConfigSettings

func main() {
	var err error
	configFile := "config.yaml"
	if len(os.Args) == 1 {
		fmt.Println("no configuration file specified, defaulting to config.yaml")
	}
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	// Get the config file on startup to prevent repeated IO/ parsing
	yamlData, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatal(err)
	}

	Config = ConfigSettings{}

	err = yaml.Unmarshal(yamlData, &Config)
	if err != nil {
		log.Fatal(err)
	}

	if len(Config.Port) == 0 {
		fmt.Println("no port specified in the yaml config file, defaulting to 8000")
		Config.Port = "8000"
	}

	if len(Config.Repos) == 0 {
		log.Fatal(errors.New("no git repos were configured in the yaml config file, at least one repo must be listed for the service to query"))
	}

	http.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		var err error

		if r.Method != "GET" {
			errorResp(w, fmt.Sprintf("incorect request method %s only the GET method is allowed", r.Method))
			return
		}

		req := &SearchRequest{}
		req.SearchTerm = r.URL.Query().Get("q")
		if len(req.SearchTerm) == 0 {
			errorResp(w, "no search term was found, query string must have a 'q' parameter which must be at least 1 character long")
			return
		}
		req.User = r.URL.Query().Get("user")

		searchResp, err := search(req)
		if err != nil {
			errorResp(w, fmt.Sprintf("search query could not be compleated, %s", err))
			return
		}

		respJSON, err := json.Marshal(searchResp)
		if err != nil {
			errorResp(w, fmt.Sprintf("could not marshal query response %s", err))
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.Write(respJSON)
	})

	fmt.Println("starting service on port :" + Config.Port)
	log.Fatal(http.ListenAndServe(":"+Config.Port, nil))
}

type SearchRequest struct {
	SearchTerm string `json:"SearchTerm"`
	User       string `json:"User"`
}

type Result struct {
	FileURL string `json:"fileURL"`
	Repo    string `json:"repo"`
}

type SearchResponse struct {
	Results []Result
}

func search(req *SearchRequest) (*SearchResponse, error) {

	url := "https://api.github.com/search/code?q=" + req.SearchTerm + "+"

	// prevent lots of heap reallocations
	repos := make([]string, 0, len(Config.Repos))
	for _, repo := range Config.Repos {
		// if a user was specified filter only by that specific user
		if repo[:len(req.User)] == req.User && repo[len(req.User)] == '/' {
			repos = append(repos, "repo:"+repo)
		}
	}
	if len(repos) == 0 {
		// we check for 0 repo case when we load the config file so this is a filtring error
		return nil, fmt.Errorf("no repositories were found belonging to the user %s", req.User)
	}
	url += strings.Join(repos, "+")
	if len(url) > 256 {
		// this is a restriction of the github api
		return nil, fmt.Errorf("query must be 256 characters or less, calculated query was %s", url)
	}

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()
	if err != nil {
		return nil, err
	}

	// these structs are used to mimic the nesting of the response data
	type Repo struct {
		FullName string `json:"full_name"`
	}
	type Item struct {
		Url        string `json:"url"`
		Repository Repo   `json:"repository"`
	}
	type Error struct {
		Msg string `json:"message"`
	}
	respStruct := &struct {
		Errors []Error `json:"errors"`
		Items  []Item  `json:"items"`
	}{}

	err = json.Unmarshal(body, respStruct)
	if err != nil {
		return nil, err
	}

	if len(respStruct.Errors) > 0 {
		return nil, fmt.Errorf("there were one or more errors with the api request: %+v", respStruct.Errors)
	}

	ret := &SearchResponse{}
	for _, res := range respStruct.Items {
		ret.Results = append(
			ret.Results,
			Result{
				FileURL: res.Url,
				Repo:    res.Repository.FullName,
			},
		)
	}

	return ret, nil
}

func errorResp(w http.ResponseWriter, msg string) {
	errJSON, _ := json.Marshal(&struct {
		Error   bool
		Message string
	}{true, msg})

	w.Header().Add("Content-Type", "application/json")
	w.Write(errJSON)
}
