package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

// ConfigSettings contains the necessary configuration data for the service
type ConfigSettings struct {
	Port  int      `yaml:"port"`
	Repos []string `yaml:"repos"`
}

// Config holds the global configuration for the service
var Config *ConfigSettings

func main() {
	configFile := "config.yaml"
	if len(os.Args) == 1 {
		log.Println("no configuration file specified, defaulting to config.yaml")
	}
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	// Get the config file on startup to prevent repeated IO/ parsing
	yamlData, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatal(err)
	}

	Config = &ConfigSettings{}

	err = yaml.Unmarshal(yamlData, &Config)
	if err != nil {
		log.Fatal(err)
	}

	if Config.Port == 0 {
		log.Println("no port specified in the yaml config file, defaulting to 8000")
		Config.Port = 8000
	}

	if len(Config.Repos) == 0 {
		log.Fatal(errors.New("no git repos were configured in the yaml config file, at least one repo must be listed for the service to query"))
	}

	// Register the search endpoint here
	http.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			errorResp(w, http.StatusMethodNotAllowed, fmt.Sprintf("incorect request method %s only the GET method is allowed", r.Method))
			return
		}

		req := &SearchRequest{}
		req.SearchTerm = r.URL.Query().Get("q")
		if req.SearchTerm == "" {
			errorResp(w, http.StatusBadRequest, "no search term was found, query string must have a 'q' parameter which must be at least 1 character long")
			return
		}
		req.User = r.URL.Query().Get("user")

		searchResp, err := search(req)
		if err != nil {
			errorResp(w, http.StatusInternalServerError, fmt.Sprintf("search query could not be completed, %s", err))
			return
		}

		respJSON, err := json.Marshal(searchResp)
		if err != nil {
			errorResp(w, http.StatusInternalServerError, fmt.Sprintf("could not marshal query response %s", err))
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.Write(respJSON)
	})

	log.Println("starting service on port :" + strconv.Itoa(Config.Port))
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(Config.Port), nil))
}

// SearchRequest represents a request made by a client to this service
type SearchRequest struct {
	SearchTerm string
	User       string
}

// Result represents a single result returned from a query to this service
type Result struct {
	FileURL string
	Repo    string
}

// SearchResponse represents a collection of results and is the standard struct returned from this service
type SearchResponse struct {
	Results []*Result
}

// AddResult appends results to the search response
func (resp *SearchResponse) AddResult(result *Result) {
	resp.Results = append(resp.Results, result)
}

func search(req *SearchRequest) (*SearchResponse, error) {
	u, err := buildURL(req)
	if err != nil {
		return nil, err
	}

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// githubResponse mimics the structure of the response received from github
	type githubResponse struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Items []struct {
			URL        string `json:"html_url"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		} `json:"items"`
	}

	respStruct := &githubResponse{}

	err = json.Unmarshal(body, respStruct)
	if err != nil {
		return nil, err
	}

	if len(respStruct.Errors) > 0 {
		return nil, fmt.Errorf("there were one or more errors with the API request: %+v", respStruct.Errors)
	}

	ret := &SearchResponse{
		Results: make([]*Result, 0, len(respStruct.Items)),
	}
	for _, res := range respStruct.Items {
		ret.AddResult(
			&Result{res.URL, res.Repository.FullName},
		)
	}

	return ret, nil
}

func buildURL(req *SearchRequest) (*url.URL, error) {
	u := &url.URL{
		Scheme: "https",
		Host:   "api.github.com",
		Path:   "search/code",
	}
	q := u.Query()
	q.Set("q", req.SearchTerm)

	var repoCount int
	for _, repo := range Config.Repos {
		// if a user was specified filter only by that specific user
		// the user name must be both the prefix and of the correct length which is why we check for the / char
		if strings.HasPrefix(repo, req.User) {
			// this prevents bugs caused when one user name is a prefix of another (e.g. bja & bjatkin)
			if req.User != "" && repo[len(req.User)] != '/' {
				continue
			}
			repoCount++
			q.Add("q", "repo:"+repo)
		}
	}
	if repoCount == 0 {
		// we check for the 0 repo case when we load the config file so this is a filtering error
		return nil, fmt.Errorf("no repositories were found belonging to the user %s", req.User)
	}

	// combine all the queries together so the repos get filtered correctly
	q["q"] = []string{strings.Join(q["q"], " ")}
	u.RawQuery = q.Encode()
	if len(u.RawQuery) > 256 {
		// this is a restriction of the github api
		return nil, fmt.Errorf("query must be 256 characters or less, calculated query was %s", q)
	}

	fmt.Println("URL: ", u.String())

	return u, nil
}

// ErrorResponse represents the structure of all json reponses sent to the client in the case of an error
// the Error field will always be set to true to make error checking more convenient on the client side
type ErrorResponse struct {
	Error   bool
	Message string
}

func errorResp(w http.ResponseWriter, errorCode int, msg string) {
	errJSON, _ := json.Marshal(&ErrorResponse{true, msg})

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(errorCode)
	w.Write(errJSON)
}
