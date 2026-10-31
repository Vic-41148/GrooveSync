// Package navidrome mirrors a Navidrome library over the OpenSubsonic API.
// syncd keeps its own canonical ids; Navidrome ids, paths and hashes are only
// mapped attributes, because Navidrome rewrites its ids across major versions.
package navidrome

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apiVersion    = "1.16.1"
	clientName    = "groovesync"
	albumPageSize = 500
)

// Album is one Navidrome album.
type Album struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Artist    string `json:"artist"`
	SongCount int    `json:"songCount"`
}

// Song is one Navidrome track with the metadata the mirror stores.
type Song struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Artist          string `json:"artist"`
	Album           string `json:"album"`
	DurationSeconds int    `json:"duration"`
	Path            string `json:"path"`
	Suffix          string `json:"suffix"`
	SizeBytes       int64  `json:"size"`
	MusicBrainzID   string `json:"musicBrainzId"`
	ISRC            string `json:"isrc"`
}

// APIError is a Subsonic error response.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("navidrome: api error %d: %s", e.Code, e.Message)
}

// Client talks to one Navidrome instance. HTTP and Salt are injected so tests
// can use a local server and a predictable salt.
type Client struct {
	BaseURL  string
	User     string
	Password string
	HTTP     *http.Client
	Salt     func() (string, error)
}

// NewClient validates the connection details. A URL is required; credentials
// are checked when a request is made, so a misconfigured account reports the
// server's error rather than a local guess.
func NewClient(baseURL, user, password string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("navidrome: parse base url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("navidrome: base url must be http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("navidrome: base url must include a host")
	}
	return &Client{
		BaseURL:  strings.TrimRight(parsed.String(), "/"),
		User:     user,
		Password: password,
	}, nil
}

// Ping checks that the server answers the Subsonic handshake.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.get(ctx, "ping", nil)
	return err
}

// Health reports whether the server is reachable for the health panel.
func (c *Client) Health(ctx context.Context) error {
	return c.Ping(ctx)
}

// Albums lists every album in name order, paging until the server runs out.
func (c *Client) Albums(ctx context.Context) ([]Album, error) {
	var out []Album
	for offset := 0; ; offset += albumPageSize {
		payload, err := c.get(ctx, "getAlbumList2", map[string]string{
			"type":   "alphabeticalByName",
			"size":   fmt.Sprint(albumPageSize),
			"offset": fmt.Sprint(offset),
		})
		if err != nil {
			return out, err
		}
		var page struct {
			Response struct {
				AlbumList2 *struct {
					Album []Album `json:"album"`
				} `json:"albumList2"`
			} `json:"subsonic-response"`
		}
		if err := json.Unmarshal(payload, &page); err != nil {
			return out, fmt.Errorf("navidrome: decode album list: %w", err)
		}
		if page.Response.AlbumList2 == nil || len(page.Response.AlbumList2.Album) == 0 {
			return out, nil
		}
		out = append(out, page.Response.AlbumList2.Album...)
		if len(page.Response.AlbumList2.Album) < albumPageSize {
			return out, nil
		}
	}
}

// Songs returns the songs of one album.
func (c *Client) Songs(ctx context.Context, albumID string) ([]Song, error) {
	if albumID == "" {
		return nil, errors.New("navidrome: album id is required")
	}
	payload, err := c.get(ctx, "getAlbum", map[string]string{"id": albumID})
	if err != nil {
		return nil, err
	}
	var album struct {
		Response struct {
			Album *struct {
				Song []Song `json:"song"`
			} `json:"album"`
		} `json:"subsonic-response"`
	}
	if err := json.Unmarshal(payload, &album); err != nil {
		return nil, fmt.Errorf("navidrome: decode album: %w", err)
	}
	if album.Response.Album == nil {
		return nil, nil
	}
	return album.Response.Album.Song, nil
}

func (c *Client) get(ctx context.Context, method string, params map[string]string) ([]byte, error) {
	if c == nil {
		return nil, errors.New("navidrome: client is required")
	}
	if c.BaseURL == "" {
		return nil, errors.New("navidrome: base url is required")
	}
	salt, err := c.salt()
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("u", c.User)
	query.Set("t", token(c.Password, salt))
	query.Set("s", salt)
	query.Set("v", apiVersion)
	query.Set("c", clientName)
	query.Set("f", "json")
	for key, value := range params {
		query.Set(key, value)
	}
	endpoint := c.BaseURL + "/rest/" + method + ".view?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("navidrome: build request: %w", err)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("navidrome: request %s: %w", method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("navidrome: read %s: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("navidrome: %s returned %s", method, resp.Status)
	}

	var envelope struct {
		Response struct {
			Status string    `json:"status"`
			Error  *APIError `json:"error"`
		} `json:"subsonic-response"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("navidrome: decode %s: %w", method, err)
	}
	if envelope.Response.Status != "ok" {
		if envelope.Response.Error != nil {
			return nil, envelope.Response.Error
		}
		return nil, fmt.Errorf("navidrome: %s returned status %q", method, envelope.Response.Status)
	}
	return body, nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) salt() (string, error) {
	if c.Salt != nil {
		return c.Salt()
	}
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("navidrome: generate salt: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func token(password, salt string) string {
	sum := md5.Sum([]byte(password + salt))
	return hex.EncodeToString(sum[:])
}
