package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Constants for timeouts
const (
	readTimeout  = 10 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 60 * time.Second
)

// addHTTPPrefix ensures the URL has a valid scheme (http or https)
func addHTTPPrefix(url string) string {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		// Default to http if no scheme is provided
		return "http://" + url
	}
	return url
}

// isValidURL validates the URL format
func isValidURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

// fetchHTMLContent fetches the HTML content from the given URL with retries and timeouts
func fetchHTMLContent(ctx context.Context, url string) (string, error) {
	url = addHTTPPrefix(url)
	if !isValidURL(url) {
		return "", errors.New("invalid URL")
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch page: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch page, status code: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}

	return string(body), nil
}

// parseHTMLForFavicon parses the HTML to find the most likely favicon URL
func parseHTMLForFavicon(htmlContent string, baseURL string) (string, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %v", err)
	}

	var faviconURL string
	var largestIconURL string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "link" {
			var rel, href string
			var sizeAttr string
			for _, attr := range n.Attr {
				switch attr.Key {
				case "rel":
					rel = attr.Val
				case "href":
					href = attr.Val
				case "sizes":
					sizeAttr = attr.Val
				}
			}

			// Prioritize rel="icon" and rel="shortcut icon"
			if strings.Contains(rel, "icon") && href != "" {
				if strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") {
					href = baseURL + href
				} else if strings.HasPrefix(href, "//") {
					href = "http:" + href
				}

				// Check for largest icon if sizes are available
				if rel == "icon" && sizeAttr != "" {
					largestIconURL = href // Update to largest icon URL
				}

				// Set faviconURL to the first icon we find
				if faviconURL == "" {
					faviconURL = href
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}

	f(doc)

	// Prefer the largest icon URL if available
	if largestIconURL != "" {
		return largestIconURL, nil
	}

	if faviconURL == "" {
		// If no favicon is found in the HTML, try a common fallback
		return baseURL + "/favicon.ico", nil
	}

	return faviconURL, nil
}

// getBaseURL extracts the base URL from a full URL
func getBaseURL(url string) string {
	splitIndex := strings.Index(url, "//") + 2
	endIndex := strings.Index(url[splitIndex:], "/")
	if endIndex != -1 {
		return url[:splitIndex+endIndex]
	}
	return url
}

// handleRequest handles incoming HTTP requests, fetches the HTML content, and finds the favicon
func handleRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Validate and get the 'url' query parameter from the GET request
	url := r.URL.Query().Get("url")
	if url == "" {
		http.Error(w, "Missing 'url' query parameter", http.StatusBadRequest)
		return
	}

	// Fetch HTML content with context
	htmlContent, err := fetchHTMLContent(ctx, url)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch HTML: %v", err), http.StatusInternalServerError)
		return
	}

	// Get the base URL to handle relative favicon paths
	baseURL := getBaseURL(url)

	// Parse the HTML to find the favicon URL
	faviconURL, err := parseHTMLForFavicon(htmlContent, baseURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to find favicon: %v", err), http.StatusNotFound)
		return
	}

	// Write the favicon URL as the response
	w.Header().Set("Content-Type", "text/plain")
	_, err = w.Write([]byte(faviconURL))
	if err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
	}
}

// main starts the HTTP server and handles graceful shutdown
func main() {
	// Set up the server with timeouts and graceful shutdown
	srv := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		Handler:      http.TimeoutHandler(http.HandlerFunc(handleRequest), 5*time.Second, "Request timed out"),
	}

	// Run the server in a goroutine to allow graceful shutdown
	go func() {
		fmt.Println("Server is running on port 8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Could not listen on %s: %v\n", srv.Addr, err)
		}
	}()

	// Graceful shutdown on interrupt
	shutdownServer(srv)
}

// shutdownServer gracefully shuts down the HTTP server
func shutdownServer(srv *http.Server) {
	// Listen for interrupt signals to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	fmt.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	fmt.Println("Server stopped gracefully.")
}
