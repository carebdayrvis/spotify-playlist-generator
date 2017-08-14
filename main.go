package main

import (
	"encoding/gob"
	"fmt"
	"github.com/Pallinder/go-randomdata"
	"github.com/zmb3/spotify"
	"golang.org/x/oauth2"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"
)

const PORT string = ":9001"
const PLAYLIST_NAME = "Random Recs"

type info struct {
	Templates       map[string]string
	RecentlyPlayed  []spotify.RecentlyPlayedItem
	Recommendations *spotify.Recommendations
	Token           *oauth2.Token
	Albums          []spotify.SavedAlbum
	PlaylistID      spotify.ID
}

var client *spotify.Client
var cache info
var redirect string

func main() {
	var state string

	redirect = os.Getenv("REDIRECT")

	if redirect == "" {
		log.Fatal("Please speify a REDIRECT env variable")
	}

	scopes := []string{"playlist-read-private", "playlist-modify-private", "user-library-read", "ugc-image-upload", "user-read-recently-played"}
	auth := spotify.NewAuthenticator(redirect, scopes...)

	err := loadCache()
	if err != nil {
		log.Fatal(err)
	}

	if cache.Token != nil {
		tmpClient := auth.NewClient(cache.Token)
		client = &tmpClient
	}

	http.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		token, err := auth.Token(state, r)
		if err != nil {
			http.Error(w, "Couldn't get token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		cache.Token = token
		err = saveCache()
		if err != nil {
			log.Println("Error saving cache:", err)
		}

		tmpClient := auth.NewClient(token)
		client = &tmpClient

		http.Redirect(w, r, "/home", 302)
	})

	http.HandleFunc("/generate", func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			http.Redirect(w, r, "/connect", 302)
			return
		}

		err := loadCache()
		if err != nil {
			http.Error(w, "Problem generating playlist: "+err.Error(), http.StatusInternalServerError)
			return
		}

		cache.Recommendations = nil
		opts := r.URL.Query()

		replace, err := strconv.ParseBool(opts.Get("replace"))
		if err != nil {
			http.Error(w, "Problem generating playlist: "+err.Error(), http.StatusInternalServerError)
			return
		}

		seed, err := strconv.ParseBool(opts.Get("seed"))
		if err != nil {
			http.Error(w, "Problem generating playlist: "+err.Error(), http.StatusInternalServerError)
			return
		}

		err = generate(*client, replace, seed)
		if err != nil {
			http.Error(w, "Problem generating playlist: "+err.Error(), http.StatusInternalServerError)
			return
		}

		fmt.Fprint(w, "Playlist saved.")
	})

	http.HandleFunc("/home", func(w http.ResponseWriter, r *http.Request) {
		var template string

		if cache.Templates == nil {
			cache.Templates = map[string]string{}
		}

		if tpl, ok := cache.Templates["index"]; ok {
			template = tpl
		} else {
			tpl, err := ioutil.ReadFile("./templates/index.html")
			if err != nil {
				http.Error(w, "Error reading template file: "+err.Error(), http.StatusInternalServerError)
				return
			}
			// TODO: cache template
			cache.Templates["index"] = string(tpl)
			template = string(tpl)
		}

		fmt.Fprint(w, template)
	})

	http.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		state = time.Now().Format(time.RFC3339)
		url := auth.AuthURL(state)
		http.Redirect(w, r, url, 302)
	})

	fmt.Println("Listening on", PORT)
	log.Fatal(http.ListenAndServe(PORT, nil))
}

func savePlaylist(client spotify.Client, tracks []spotify.SimpleTrack, replace bool) error {
	var hasPlaylist bool = false
	var playlist spotify.SimplePlaylist

	currentUser, err := client.CurrentUser()
	if err != nil {
		return err
	}

	id := currentUser.User.ID

	playlistsPage, err := client.CurrentUsersPlaylists()
	if err != nil {
		return err
	}

	for _, p := range playlistsPage.Playlists {
		if p.ID == cache.PlaylistID {
			hasPlaylist = true
			playlist = p
		}
	}

	if !hasPlaylist {
		tmp, err := client.CreatePlaylistForUser(id, PLAYLIST_NAME, false)
		if err != nil {
			return err
		}

		playlist = tmp.SimplePlaylist
		cache.PlaylistID = playlist.ID
	}

	trackIDs := []spotify.ID{}

	for i := range tracks {
		trackIDs = append(trackIDs, tracks[i].ID)
	}

	if hasPlaylist && replace {
		err := client.ReplacePlaylistTracks(id, playlist.ID, trackIDs...)
		if err != nil {
			return err
		}
		fmt.Println(time.Now().Format("01-02-06 15:06"), "Replaced playlist.")
	} else {
		snapshotID, err := client.AddTracksToPlaylist(id, playlist.ID, trackIDs...)
		if err != nil {
			return err
		}
		fmt.Println(time.Now().Format("01-02-06 15:06"), "Saved playlist.", snapshotID)
	}

	return saveCache()
}

func loadCache() error {
	var err error
	var f *os.File
	f, err = os.OpenFile("./cache", os.O_RDWR, 0666)
	if err != nil && !os.IsNotExist(err) {
		return err
	} else if os.IsNotExist(err) {
		f, err = os.Create("./cache")
		if err != nil {
			return err
		}
	}

	defer f.Close()

	decoder := gob.NewDecoder(f)
	err = decoder.Decode(&cache)

	fmt.Printf("Cache: \nPlaylistID: %+v\n", cache.PlaylistID)

	if err != nil && err != io.EOF {
		return err
	}

	return nil
}

func saveCache() error {
	var err error
	var f *os.File
	f, err = os.OpenFile("./cache", os.O_RDWR, 0666)
	if err != nil && !os.IsNotExist(err) {
		return err
	} else if os.IsNotExist(err) {
		f, err = os.Create("./cache")
		if err != nil {
			return err
		}
	}

	encoder := gob.NewEncoder(f)

	return encoder.Encode(&cache)
}

func generate(client spotify.Client, replace, seedFlag bool) error {
	var tracks []spotify.SimpleTrack
	// Get albums first
	library, err := loadLibrary(client)
	if err != nil {
		return err
	}

	seed, err := getSeedFromRecentlyPlayed(client, seedFlag)
	if err != nil {
		return err
	}

	// Get recommendations
	if cache.Recommendations == nil {
		res, err := client.GetRecommendations(seed, &spotify.TrackAttributes{}, &spotify.Options{})
		if err != nil {
			return err
		}

		// Cache recommendations
		cache.Recommendations = res
		err = saveCache()
		if err != nil {
			return err
		}
	}

	tracks = cache.Recommendations.Tracks

	// Create map of artists by id
	artistMap := map[spotify.ID]string{}
	for i := range library {
		album := library[i].FullAlbum
		for j := range album.Artists {
			id := album.Artists[j].ID
			if _, ok := artistMap[id]; !ok {
				artistMap[id] = album.Artists[j].Name
			}
		}
	}

	filteredTracks := []spotify.SimpleTrack{}

	for i := range tracks {
		track := tracks[i]
		for j := range track.Artists {
			id := track.Artists[j].ID
			if _, ok := artistMap[id]; !ok {
				filteredTracks = append(filteredTracks, tracks[i])
			}

		}
	}

	return savePlaylist(client, filteredTracks, replace)
}

func getSeedFromRecentlyPlayed(client spotify.Client, seedFlag bool) (spotify.Seeds, error) {
	var recent []spotify.RecentlyPlayedItem
	var seeds spotify.Seeds

	// Get last 50 played songs
	if cache.RecentlyPlayed == nil && seedFlag {
		recent, err := client.PlayerRecentlyPlayedOpt(&spotify.RecentlyPlayedOptions{Limit: 50})
		if err != nil {
			return seeds, err
		}

		// Cache recently played for now
		cache.RecentlyPlayed = recent
		err = saveCache()
		if err != nil {
			return seeds, err
		}
	} else if !seedFlag {
		// TODO: finish this
		p := randomdata.Paragraph()
		return seeds, fmt.Errorf("%v\n", p)

	}
	recent = cache.RecentlyPlayed

	if len(recent) < 1 {
		log.Fatal(fmt.Sprintf("Recent was len of %v", len(recent)))
	}

	max := len(recent)
	rand.Seed(time.Now().Unix())

	usedIndexes := map[int]bool{}

	for i := 0; i < 5; i++ {
	Index:
		for {
			trackIndex := rand.Intn(max)
			if _, ok := usedIndexes[trackIndex]; !ok {
				seeds.Tracks = append(seeds.Tracks, recent[trackIndex].Track.ID)
				usedIndexes[trackIndex] = true
				break Index
			} else {
				continue Index
			}
		}
	}

	return seeds, nil
}

func loadLibrary(client spotify.Client) ([]spotify.SavedAlbum, error) {
	var albums []spotify.SavedAlbum

	if len(cache.Albums) > 0 {
		return cache.Albums, nil
	}

	limit := 50
	offset := 0

	for {
		fmt.Println("Getting page", offset+1, "of albums")
		thisOffset := offset * limit
		res, err := client.CurrentUsersAlbumsOpt(&spotify.Options{Limit: &limit, Offset: &thisOffset})
		if err != nil {
			return albums, err
		}

		albums = append(albums, res.Albums...)

		if len(res.Albums) > 0 {
			offset++
		} else {
			break
		}
	}

	cache.Albums = albums

	return albums, saveCache()
}
