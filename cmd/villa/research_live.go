package main

// research_live.go binds the research task's web reach to the host (ADR-0045):
// SearXNG and villa-websafe publish no host port, so both are called the way every
// other in-network service is, by the probe doctrine's fixed-arg `podman run` of the
// helper curl on villa.network (probeCurl). The model is the inference client's, the
// same one the grounding audit uses (ADR-0014).
//
// Nothing the task says goes on curl's argv. The search words, the page URLs and the
// websafe bearer travel in a curl config read from stdin (-K -), so another local
// user cannot read them from /proc (#252's reason for `-H @-`). The values are
// quoted by curlConfigQuote, so a quote or a line break in a query cannot end the
// value and add an option of its own.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/research"
	"github.com/MatrixMagician/VillaStraylight/internal/taskrun"
	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

// webCurl runs one in-network curl on villa.network: a curl config on stdin and
// curl's own arguments in, curl's stdout and exit code out.
type webCurl func(ctx context.Context, stdin []byte, args []string) (stdout []byte, exitCode int, err error)

// webCompleteFn is one chat completion on the served model.
type webCompleteFn func(ctx context.Context, cfg config.VillaConfig, req llm.ChatRequest) (string, error)

// liveWebDeps is webDeps on the host: probeCurl from the helper image, and the
// inference client for the model.
func liveWebDeps(inf inference.Client) taskrun.WebDeps {
	helper := orchestrate.EmbedImage() // probe helper, never a pin (spec §7.1)
	client := inf.Chat(research.CompleteTimeout)
	return webDeps(
		func(ctx context.Context, stdin []byte, args []string) ([]byte, int, error) {
			return probeCurl(ctx, routedNetwork, helper, stdin, args)
		},
		func(ctx context.Context, cfg config.VillaConfig, req llm.ChatRequest) (string, error) {
			req.Model = cfg.Model
			var sb strings.Builder
			err := client.StreamChat(ctx, req, func(delta string) error {
				sb.WriteString(delta)
				return nil
			})
			return sb.String(), err
		},
	)
}

// webDeps binds the three calls to a curl seam and a completion seam, so the
// requests and the parsing are tested without podman.
func webDeps(curl webCurl, complete webCompleteFn) taskrun.WebDeps {
	return taskrun.WebDeps{
		Search: func(ctx context.Context, _ config.VillaConfig, query string) ([]string, error) {
			return searxngSearch(ctx, curl, query)
		},
		Fetch: func(ctx context.Context, cfg config.VillaConfig, urls []string) ([]research.Page, error) {
			return websafeLoad(ctx, curl, cfg.WebLoaderSecret, urls)
		},
		Complete: complete,
	}
}

// searxngSearch asks SearXNG for one query and returns its http(s) result URLs.
// Titles and snippets are dropped here: they are attacker-reachable text, and
// nothing downstream of this function is allowed to see them.
func searxngSearch(ctx context.Context, curl webCurl, query string) ([]string, error) {
	cfg := "silent\nfail\nget\n" +
		"url = " + curlConfigQuote(fmt.Sprintf("http://%s:%d/search", config.SearxngAddr, config.SearxngPort)) + "\n" +
		"data-urlencode = " + curlConfigQuote("q="+query) + "\n" +
		"data-urlencode = " + curlConfigQuote("format=json") + "\n"
	out, code, err := curl(ctx, []byte(cfg), []string{"-K", "-"})
	if err != nil {
		return nil, fmt.Errorf("SearXNG did not answer (curl exit %d): %w", code, err)
	}
	var reply struct {
		Results []struct {
			URL string `json:"url"`
		} `json:"results"`
		// Each entry is [engine, reason].
		Unresponsive [][]string `json:"unresponsive_engines"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		return nil, fmt.Errorf("SearXNG answered something that is not its JSON: %w", err)
	}
	var urls []string
	for _, r := range reply.Results {
		if u, err := url.Parse(r.URL); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			urls = append(urls, r.URL)
		}
	}
	// No hits while engines refused is the search not answering, not an empty
	// answer: a rate limit or a CAPTCHA at every engine looks the same as a
	// question nobody wrote about unless the engines are named.
	if len(urls) == 0 && len(reply.Unresponsive) > 0 {
		return nil, fmt.Errorf("SearXNG's search engines refused: %s", refusedEngines(reply.Unresponsive))
	}
	return urls, nil
}

// refusedEngines renders SearXNG's unresponsive_engines for a terminal: at most
// four engines, each name and reason stripped of control characters and cut short.
func refusedEngines(entries [][]string) string {
	const maxEngines, maxField = 4, 60
	clean := func(v string) string {
		v = strings.Map(func(r rune) rune {
			if r < 0x20 || (r >= 0x7f && r < 0xa0) {
				return -1
			}
			return r
		}, v)
		if r := []rune(v); len(r) > maxField {
			v = string(r[:maxField]) + "…"
		}
		return v
	}
	var parts []string
	for _, e := range entries {
		if len(parts) == maxEngines {
			parts = append(parts, "…")
			break
		}
		switch len(e) {
		case 0:
		case 1:
			parts = append(parts, clean(e[0]))
		default:
			parts = append(parts, clean(e[0])+" ("+clean(e[1])+")")
		}
	}
	return strings.Join(parts, ", ")
}

// websafeLoad posts the URLs to villa-websafe's /load as Open WebUI does, with the
// bearer when the host has one. The loader omits a URL it skipped, so fewer pages
// than URLs is normal; an error means the loader itself did not answer.
func websafeLoad(ctx context.Context, curl webCurl, secret string, urls []string) ([]research.Page, error) {
	body, err := json.Marshal(websafe.LoadRequest{URLs: urls})
	if err != nil {
		return nil, err
	}
	cfg := "silent\nfail\n" +
		"url = " + curlConfigQuote(fmt.Sprintf("http://%s:%d%s", config.WebsafeAddr, config.WebsafePort, websafe.LoadPath)) + "\n" +
		"header = " + curlConfigQuote("Content-Type: application/json") + "\n"
	if secret != "" {
		cfg += "header = " + curlConfigQuote("Authorization: Bearer "+secret) + "\n"
	}
	cfg += "data = " + curlConfigQuote(string(body)) + "\n"
	out, code, err := curl(ctx, []byte(cfg), []string{"-K", "-"})
	if err != nil {
		return nil, fmt.Errorf("villa-websafe did not answer (curl exit %d): %w", code, err)
	}
	var reply []struct {
		PageContent string `json:"page_content"`
		Metadata    struct {
			Source string `json:"source"`
			Title  string `json:"title"`
			Guard  struct {
				Detected bool     `json:"detected"`
				Rules    []string `json:"rules"`
			} `json:"guard"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		return nil, fmt.Errorf("villa-websafe answered something that is not its JSON: %w", err)
	}
	pages := make([]research.Page, 0, len(reply))
	for _, r := range reply {
		pages = append(pages, research.Page{
			URL: r.Metadata.Source, Title: r.Metadata.Title, Content: r.PageContent,
			Flagged: r.Metadata.Guard.Detected, Rules: r.Metadata.Guard.Rules,
		})
	}
	return pages, nil
}

// curlConfigQuote renders s as one quoted curl config value. Inside the quotes curl
// reads \\, \", \t, \n, \r and \v as escapes, so those are the characters that need
// one; a raw line break would end the option and let the rest of s be read as
// another.
func curlConfigQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`, "\v", `\v`).Replace(s) + `"`
}
