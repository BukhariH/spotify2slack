package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/joho/godotenv"
	"github.com/nlopes/slack"
	"github.com/robfig/cron"
	"github.com/zmb3/spotify"
)

var (
	spotifyCallbackUrl string
	state              string
	slackSymbol        string
	spotifyAuth        spotify.Authenticator
	spotifyClient      spotify.Client
	playerState        *spotify.PlayerState
	slackApi           *slack.Client
)

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Init Spotify Client
	spotifyClientId := os.Getenv("SPOTIFY_CLIENT_ID")
	spotifyClientSecret := os.Getenv("SPOTIFY_CLIENT_SECRET")
	spotifyCallbackUrl = os.Getenv("SPOTIFY_CALLBACK_URL")
	spotifyAuth = spotify.NewAuthenticator(spotifyCallbackUrl, spotify.ScopeUserReadCurrentlyPlaying, spotify.ScopeUserReadPlaybackState, spotify.ScopeUserModifyPlaybackState)
	spotifyAuth.SetAuthInfo(spotifyClientId, spotifyClientSecret)
	state, _ = newUUID()

	// Init Slack Client
	slackSymbol = os.Getenv("SLACK_SYMBOL")
	slackToken := os.Getenv("SLACK_TOKEN")
	slackApi = slack.New(slackToken)

	r := chi.NewRouter()

	// A good base middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Routes
	r.Get("/", index)
	r.Get("/callback", completeSpotifyAuth)
	r.Get("/currentlyPlaying", currentlyPlaying)

	fmt.Print("🚀 We're live & listening!\n")

	port := os.Getenv("PORT")
	http.ListenAndServe(port, r)
}

func index(w http.ResponseWriter, r *http.Request) {
	url := spotifyAuth.AuthURL(state)
	http.Redirect(w, r, url, http.StatusFound)
}

func currentlyPlaying(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	m := setCurrentlyPlaying()
	json.NewEncoder(rw).Encode(m)
}

func completeSpotifyAuth(wr http.ResponseWriter, req *http.Request) {
	tok, err := spotifyAuth.Token(state, req)
	if err != nil {
		http.Error(wr, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := req.FormValue("state"); st != state {
		http.NotFound(wr, req)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}
	// use the token to get an authenticated client
	spotifyClient = spotifyAuth.NewClient(tok)
	c := cron.New()
	c.AddFunc("@every 1m", func() {
		setCurrentlyPlaying()
	})
	c.Start()
	http.Redirect(wr, req, "/currentlyPlaying", http.StatusFound)
}

func setCurrentlyPlaying() map[string]string {
	playerState, err := spotifyClient.PlayerState()
	if err != nil {
		log.Fatal(err)
	}

	var m = make(map[string]string)
	if playerState.CurrentlyPlaying.Playing {
		m["currentlyListeningTo"] = playerState.CurrentlyPlaying.Item.Name + " - " + playerState.CurrentlyPlaying.Item.Artists[0].Name
		slackApi.SetUserCustomStatus(m["currentlyListeningTo"], slackSymbol)
	} else {
		m["currentlyListeningTo"] = "Not listening to anything right now"
		slackApi.UnsetUserCustomStatus()
	}
	return m
}

// https://play.golang.org/p/4FkNSiUDMg
// newUUID generates a random UUID according to RFC 4122
// Using a UUID for the spotify api state
func newUUID() (string, error) {
	uuid := make([]byte, 16)
	n, err := io.ReadFull(rand.Reader, uuid)
	if n != len(uuid) || err != nil {
		return "", err
	}
	// variant bits; see section 4.1.1
	uuid[8] = uuid[8]&^0xc0 | 0x80
	// version 4 (pseudo-random); see section 4.1.3
	uuid[6] = uuid[6]&^0xf0 | 0x40
	return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:]), nil
}
