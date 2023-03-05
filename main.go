package main

import (
	"bytes"
	"encoding/json"
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

var baseUrl string = "https://www.simplyhired.co.uk/"
var keyPattern, patternErr = regexp.Compile(`/job/(.*)\?`)

func worker(id int, jobs <-chan scrapeJob, ack chan<- bool) {
	log.Printf("started worker %d", id)

	for scrapeJob := range jobs {
		url := fmt.Sprintf(
			"%ssearch?q=%s&l=%s&pn=%d&from=pagination",
			baseUrl,
			url.QueryEscape(scrapeJob.query),
			url.QueryEscape(scrapeJob.location),
			scrapeJob.page,
		)

		log.Printf("retrieving page %s\n", url)

		resp, err := http.Get(url)
		check(err)

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		check(err)
		resp.Body.Close()

		out := doc.Find(".SerpJob-link").Map(func(i int, sel *goquery.Selection) string {
			ref, _ := sel.Attr("data-mdref")
			title := sel.Text()
			parsed := map[string]string{"ref": ref, "text": title}
			bytes, err := json.Marshal(parsed)
			check(err)

			return string(bytes)
		})

		log.Printf("worker=%d found %d jobs", id, len(out))

		for _, job := range out {
			var discovered map[string]string
			json.Unmarshal([]byte(job), &discovered)

			key := keyPattern.FindStringSubmatch(discovered["ref"])[1]

			resp, err := http.Get("https://www.simplyhired.co.uk/api/job?key=" + key)
			check(err)

			read, err := io.ReadAll(resp.Body)
			check(err)
			resp.Body.Close()

			var jobResponse jobApiResponse
			json.Unmarshal(read, &jobResponse)

			filename := strings.ReplaceAll(jobResponse.Job.Title, " ", "_")
			filename = strings.ReplaceAll(filename, "/", "_")

			parentDirectory := fmt.Sprintf("./output/page_%d/", scrapeJob.page)

			// ensure the page directory exists
			os.MkdirAll(parentDirectory, os.ModePerm)

			outputFile := fmt.Sprintf("%sjob_%s", parentDirectory, filename)

			var formatted bytes.Buffer
			json.Indent(&formatted, read, "", "\t")

			writeErr := os.WriteFile(outputFile, formatted.Bytes(), 0644)
			check(writeErr)
		}

		ack <- true
	}
}

func main() {
	check(patternErr)

	args := os.Args[1:]

	query := args[0]
	loc := args[1]

	// max pages
	const pageLimit = 5

	// worker routine count
	const workers = 5

	// ensure the output location exists and is empty
	os.RemoveAll("./output/")
	os.MkdirAll("./output/", os.ModePerm)

	jobs := make(chan scrapeJob, pageLimit)
	results := make(chan bool, pageLimit)
	for w := 0; w < workers; w++ {
		go worker(w, jobs, results)
	}

	for i := 1; i <= pageLimit; i++ {
		jobs <- scrapeJob{
			location: loc,
			query:    query,
			page:     i,
		}
	}
	close(jobs)

	for a := 1; a <= pageLimit; a++ {
		// wait for each job to write its output files
		<-results
	}
}

type scrapeJob struct {
	location string
	query    string
	page     int
}

type jobApiResponse struct {
	Job jobDetail `json:"job"`
}

type jobDetail struct {
	Title string `json:"title"`
}
