# Simple Recommendations Playlist Generator

Spotify already has a Weekly Discover playlist but I've found I burn through it before the week is out. Other playlists seem to have music that I haven't heard yet, but a lot of stuff I have in my library mixed in. 

This tool creates a playlist from recently played tracks, filtering out those from artists I have saved an album from.

## Setup

Requires `go 1.8` to be installed.

Clone this repo and `cd` into it.

Set `SPOTIFY_ID` and `SPOTIFY_SECRET` env variables obtained from [Spotify Developer](https://developer.spotify.com/my-applications)

Set `REDIRECT` env variable ot whatever is setup on the Spotify App. 

Run `go build`, then `./spotify-playlist` 

Server listens on port 9001

Once authorized, the access token is cached on disk, so be aware of that. 

Recommendations will be seeded randomly from your recently played tracks

## Using

Navigate to [http://localhost:9001/generate](http://localhost:9001/generate). If not already completed, you will be taken through the authorization flow.

To delete the cache, just `rm` the `cache` file created in this repo's root.
