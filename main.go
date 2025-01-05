package main

import (
	"flag"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetFormatter(
		&log.TextFormatter{
			DisableColors: false,
			ForceColors:   true,
			FullTimestamp: true,
		},
	)
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Errorf("Failed to load environment variables %v", err)
	}

	flag.Parse()

	articles, err := FetchArticles(apiKey, *query)
	if err != nil {
		log.Errorf("Error fetching articles: %v\n", err)
		return
	}

	if err := SaveArticles("osint.json", articles); err != nil {
		log.Errorf("Error saving articles: %v\n", err)
		return
	}

	log.Println("Data saved to osint.json")

	// articles, err := LoadArticles(*input)
	if err != nil {
		log.Errorf("Error loading articles: %v\n", err)
		return
	}
	processArticles, err := processArticles(articles, *onlyScrape)
	if err != nil {
		log.Errorf("Error processing articles: %v", err)
		return
	}
	if *onlyScrape {
		if err := saveScrapedContent(*outputFile, processArticles); err != nil {
			log.Errorf("Error saving scraped content: %v", err)
			return
		}
		log.Printf("Scraped content saved to %s\n", *outputFile)
	} else {
		if err := saveProcessedArticles("labeled_articles.json", processArticles); err != nil {
			log.Errorf("Error saving processed articles: %v", err)
			return
		}
		log.Printf("Analysis complete. Results saved to labeled_articles.json")
		if err := generateSummaryReport("summary.md", processArticles); err != nil {
			log.Errorf("Error generating summary report %v", err)
		}

		log.Println("Summary report saved to summary.md")
	}
}
