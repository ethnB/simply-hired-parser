package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func check(err error) {
	if err != nil {
		log.Panic(err)
	}
}

const baseUrl string = "https://www.simplyhired.co.uk/"
const outputDir string = "./output/"

// max site pages to scrape
const pageLimit int = 2

// worker routine count
const workers int = 5

// required to parse out the job key required for the details API
var keyPattern, _ = regexp.Compile(`/job/(.*)\?`)

func worker(wid int, jobs <-chan scrapeJob, complete chan<- bool) {
	log.Printf("worker=%d started", wid)

	for scrapeJob := range jobs {
		url := fmt.Sprintf(
			"%ssearch?q=%s&l=%s&pn=%d&from=pagination",
			baseUrl,
			url.QueryEscape(scrapeJob.searchQuery),
			url.QueryEscape(scrapeJob.location),
			scrapeJob.page,
		)

		log.Printf("worker=%d retrieving page %s", wid, url)
		resp, err := http.Get(url)
		check(err)

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		check(err)
		resp.Body.Close()

		// parse the job cards in the HTML into the data we need for API queries
		jobLinks := doc.Find(".SerpJob-link").Map(func(i int, sel *goquery.Selection) string {
			ref, _ := sel.Attr("data-mdref")
			jobTitle := sel.Text()
			parsed := map[string]string{
				"ref":   ref,
				"title": jobTitle,
			}
			bytes, err := json.Marshal(parsed)
			check(err)
			return string(bytes)
		})

		jobCount := len(jobLinks)
		log.Printf("worker=%d found %d jobs", wid, jobCount)

		parentDirectory := fmt.Sprintf("%spage_%d/", outputDir, scrapeJob.page)
		log.Printf("worker=%d writing to %s", wid, parentDirectory)

		for _, job := range jobLinks {
			var discovered map[string]string
			json.Unmarshal([]byte(job), &discovered)

			ref := discovered["ref"]
			title := discovered["title"]

			key := keyPattern.FindStringSubmatch(ref)[1]

			jobApiUrl := fmt.Sprintf("https://www.simplyhired.co.uk/api/job?key=%s", key)

			log.Printf("worker=%d page=%d pulling job details for '%s'", wid, scrapeJob.page, title)
			resp, err := http.Get(jobApiUrl)
			check(err)

			read, err := io.ReadAll(resp.Body)
			check(err)
			resp.Body.Close()

			var jobResponse jobApiResponse
			json.Unmarshal(read, &jobResponse)

			// sanitize the filename somewhat
			filename := strings.ReplaceAll(jobResponse.Job.Title, " ", "_")
			filename = strings.ReplaceAll(filename, "/", "_")

			// ensure the page-level directory exists
			os.MkdirAll(parentDirectory, os.ModePerm)

			outputFile := fmt.Sprintf("%s%s", parentDirectory, filename)

			var formatted bytes.Buffer
			json.Indent(&formatted, read, "", "\t")

			writeErr := os.WriteFile(outputFile, formatted.Bytes(), 0644)
			check(writeErr)
		}

		log.Printf("worker=%d written %d jobs to %s", wid, jobCount, parentDirectory)

		complete <- true
	}
}

func main() {
	// location e.g. 'Greater Manchester'
	location := flag.String("location", "", "location to search for jobs e.g. 'Greater Manchester'")
	// search query string e.g. 'Software Engineer'
	searchQuery := flag.String("query", "", "job to search for e.g. 'Software Engineer'")

	flag.Parse()

	if *searchQuery == "" {
		log.Panic("query is a required flag (-query \"Software Engineer\")")
	}

	if *location == "" {
		log.Panic("location is a required flag (-location \"Greater Manchester\")")
	}

	// ensure the output location exists and is empty
	os.RemoveAll(outputDir)
	os.MkdirAll(outputDir, os.ModePerm)

	jobs := make(chan scrapeJob, pageLimit)
	results := make(chan bool, pageLimit)
	for w := 0; w < workers; w++ {
		go worker(w, jobs, results)
	}

	for i := 1; i <= pageLimit; i++ {
		jobs <- scrapeJob{
			location:    *location,
			searchQuery: *searchQuery,
			page:        i,
		}
	}
	close(jobs)

	for a := 1; a <= pageLimit; a++ {
		// wait for all jobs to complete
		<-results
	}
}

type scrapeJob struct {
	location    string
	searchQuery string
	page        int
}

type jobApiResponse struct {
	Job jobDetail `json:"job"`
}

type jobDetail struct {
	Title string `json:"title"`
}
