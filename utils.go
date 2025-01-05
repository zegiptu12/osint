package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	log "github.com/sirupsen/logrus"
)

type Article struct {
	Source struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"source"`
	Author      string `json:"author"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	URLToImage  string `json:"urlToImage"`
	PublishedAt string `json:"publishedAt"`
	Content     string `json:"content"`
}

type NewAPIResponse struct {
	Status       string    `json:"status"`
	TotalResults int       `json:"totalResults"`
	Articles     []Article `json:"articles"`
}

type ProcessedArticle struct {
	URL     string             `json:"url"`
	Title   string             `json:"title"`
	Content string             `json:"content"`
	Summary string             `json:"summary"`
	Label   string             `json:"label"`
	Score   float64            `json:"score"`
	Methods map[string]float64 `json:"methods"`
}

var (
	apiKey     = ""
	query      = flag.String("query", "Syrian War", "Entery query to search")
	hgfApiKey  = ""
	onlyScrape = flag.Bool("onlyScrape", false, "Only scrape articles without calling any apis")
	outputFile = flag.String("o", "./scrape.json", "Enter a path to save scraped articles")
	limit      = flag.Int("limit", 5, "enter amount of articles to scrape")
)

func FetchArticles(apiKey, query string) ([]Article, error) {
	url := fmt.Sprintf("https://newsapi.org/v2/everything?q=%s&apiKey=%s&pageSize=5", query, apiKey)
	res, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Error fetching articles: %v", err)
	}
	defer res.Body.Close()

	var newResponse NewAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&newResponse); err != nil {
		return nil, fmt.Errorf("Error decoding response: %v", err)
	}
	return newResponse.Articles, nil
}

func SaveArticles(filename string, articles []Article) error {
	data, err := json.MarshalIndent(articles, "", "  ")
	if err != nil {
		return fmt.Errorf("Error marshaling articles: %v", err)
	}
	return os.WriteFile(filename, data, 0o644)
}

func LoadArticles(filename string) ([]Article, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("Error reading file %s: %v", filename, err)
	}
	var articles []Article
	if err := json.Unmarshal(data, &articles); err != nil {
		return nil, fmt.Errorf("Error unmarshaling articles: %v", err)
	}
	return articles, nil
}

func ScrapeArticle(url string) (string, error) {
	res, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("Error fetching URL %s: %v", url, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("non-200 response code %d | status code: %v", &err, res.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return "", fmt.Errorf("error parsing HTML: %v", err)
	}

	var contentBuilder strings.Builder
	doc.Find("p").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		contentBuilder.WriteString(text + "\n")
	})

	return contentBuilder.String(), nil
}

func saveScrapedContent(filename string, articles []ProcessedArticle) error {
	var scrapedData []map[string]string

	for _, article := range articles {
		data := map[string]string{
			"url":     article.URL,
			"content": article.Content,
		}
		scrapedData = append(scrapedData, data)
	}
	data, err := json.MarshalIndent(scrapedData, "", "  ")
	if err != nil {
		return fmt.Errorf("Error marshaling scraped content %v", err)
	}
	return os.WriteFile(filename, data, 0o644)
}

func splitTextIntoChunks(text string, maxTokens int) []string {
	words := strings.Fields(text)

	var chunks []string
	for i := 0; i < len(words); i += maxTokens {
		end := i + maxTokens
		if end > len(words) {
			end = len(words)
		}
		chunk := strings.Join(words[i:end], " ")
		chunks = append(chunks, chunk)
	}
	return chunks
}

func callHuggingFaceAPI(endpoint string, payload []byte) ([]byte, error) {
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+hgfApiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request to Hugging Face API: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("non-200 response from Hugging Face API: %d - %s", res.StatusCode, string(bodyBytes))
	}

	return io.ReadAll(res.Body)
}

func callSummarizationAPI(text string) (string, error) {
	endpoint := "https://api-inference.huggingface.co/models/facebook/bart-large-cnn"
	payload := map[string]interface{}{
		"inputs": text,
		"parameters": map[string]interface{}{
			"max_length": 150,
			"min_length": 50,
			"do_sample":  false,
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	responseBytes, err := callHuggingFaceAPI(endpoint, payloadBytes)
	if err != nil {
		return "", fmt.Errorf("Error while getting response from HuggingFaceAPI endpoint: %v", err)
	}
	var result []map[string]string
	if err := json.Unmarshal(responseBytes, &result); err != nil {
		return "", fmt.Errorf("error unmarshiling summary: %v", err)
	}

	if len(result) > 0 {
		return result[0]["summary_text"], nil
	}
	return "", fmt.Errorf("empty summary")
}

func SummarizeText(text string) (string, error) {
	chunks := splitTextIntoChunks(text, 500)
	var summaryParts []string

	for _, chunk := range chunks {
		summary, err := callSummarizationAPI(chunk)
		if err != nil {
			return "", err
		}
		summaryParts = append(summaryParts, summary)
	}

	combinedSummary := strings.Join(summaryParts, " ")
	finalSummary, err := callSummarizationAPI(combinedSummary)
	if err != nil {
		return "", err
	}
	return finalSummary, nil
}

func classifyText(text string, labels []string, multiLabel bool) (map[string]float64, error) {
	endpoint := "https://api-inference.huggingface.co/models/facebook/bart-large-mnli"
	payload := map[string]interface{}{
		"inputs": text,
		"parameters": map[string]interface{}{
			"candidate_labels": labels,
			"multi_label":      multiLabel,
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	responseBytes, err := callHuggingFaceAPI(endpoint, payloadBytes)
	if err != nil {
		return nil, err
	}

	var result struct {
		Labels []string  `json:"labels"`
		Scores []float64 `json:"scores"`
	}
	if err := json.Unmarshal(responseBytes, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling classification response: %v", err)
	}

	classification := make(map[string]float64)
	for i, label := range result.Labels {
		classification[label] = result.Scores[i]
	}

	return classification, nil
}

func processArticles(articles []Article, onlyScrape bool) ([]ProcessedArticle, error) {
	var wg sync.WaitGroup
	resultsChan := make(chan ProcessedArticle, len(articles))
	limit := 10
	sem := make(chan struct{}, limit)

	for _, article := range articles {
		article := article
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			log.Printf("Processing: %s - %s\n", article.Title, article.URL)

			content, err := ScrapeArticle(article.URL)
			if err != nil {
				log.Errorf("Error scraping article: %v\n", err)
				return
			}

			if onlyScrape {
				processedArticle := ProcessedArticle{
					URL:     article.URL,
					Content: content,
				}
				resultsChan <- processedArticle
				return
			}

			summary, err := SummarizeText(content)
			if err != nil {
				log.Errorf("Error summarizing article: %v\n", err)
				return
			}

			techniques := []string{
				"name-calling", "bandwagon", "fear appeal", "appeal to authority",
				"glittering generalities", "plain folks appeal", "testimonial", "loaded language",
			}
			methods, err := classifyText(content, techniques, true)
			if err != nil {
				log.Errorf("Error classifying propaganda methods: %v\n", err)
				return
			}

			labels := []string{"propaganda", "neutral"}
			labelScores, err := classifyText(content, labels, false)
			if err != nil {
				log.Errorf("Error classifying article: %v\n", err)
				return
			}

			label := labels[0]
			score := labelScores[label]
			if labelScores[labels[1]] > score {
				label = labels[1]
				score = labelScores[label]
			}
			processedArticle := ProcessedArticle{
				URL:     article.URL,
				Title:   article.Title,
				Content: content,
				Summary: summary,
				Label:   label,
				Score:   score,
				Methods: methods,
			}
			resultsChan <- processedArticle
		}()
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var processedArticles []ProcessedArticle
	for processedArticle := range resultsChan {
		processedArticles = append(processedArticles, processedArticle)
	}
	return processedArticles, nil
}

func saveProcessedArticles(filename string, articles []ProcessedArticle) error {
	data, err := json.MarshalIndent(articles, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling processed articles: %v", err)
	}
	return os.WriteFile(filename, data, 0o644)
}

func generateSummaryReport(filename string, articles []ProcessedArticle) error {
	var reportBuilder strings.Builder
	reportBuilder.WriteString("# Propaganda Analysis Report\n\n")

	for _, article := range articles {
		reportBuilder.WriteString(fmt.Sprintf("## %s\n", article.Title))
		reportBuilder.WriteString(fmt.Sprintf("**Label**: %s\n", article.Label))
		reportBuilder.WriteString(fmt.Sprintf("**URL**: %s\n\n", article.URL))
		reportBuilder.WriteString(fmt.Sprintf("**Summary**: %s\n\n", article.Summary))
		reportBuilder.WriteString(fmt.Sprintf("**Propaganda Score**: %.2f\n", article.Score))
		reportBuilder.WriteString("**Propaganda Methods**:\n")
		for method, score := range article.Methods {
			reportBuilder.WriteString(fmt.Sprintf("- %s: %.2f\n", method, score))
		}
		reportBuilder.WriteString("\n---\n\n")
	}

	return os.WriteFile(filename, []byte(reportBuilder.String()), 0o644)
}
