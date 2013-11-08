package main

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"time"
)

type View struct {
	ComicId string
	GuestId string
}

var (
	jsComicId        = regexp.MustCompile("js/([a-z0-9]{4})$")
	imgComicId       = regexp.MustCompile("img/([a-z0-9]{4})$")
	htmlComicId      = regexp.MustCompile("html/([a-z0-9]{4})$")
	viewIdFinder     = regexp.MustCompile("([a-z0-9]{4})([a-f0-9]{32})")
	guestIdValidator = regexp.MustCompile("[a-f0-9]{32}")
	oldButtonFormat  = regexp.MustCompile("^/([0-9]*)/([0-9]).(jpg|gif|png)$")
)

var (
	cacheSince = time.Now().Format(http.TimeFormat)
	cacheUntil = time.Now().AddDate(60, 0, 0).Format(http.TimeFormat)
)

var viewQueue = make(chan *View, 1024)

func viewLogger() {
	var view *View
	var err error

	// Connect
	client := &StatsClient{}
	client.Connect("127.0.0.1:6379")

	// Listen to channel
	for {
		view = <-viewQueue
		err = client.AddView(view.ComicId, view.GuestId)

		if err != nil {
			fmt.Println("log failure. Re-queuing view")
			viewQueue <- view
		}
	}
}

// Check for cookie or generate new guest ID
func getGuestId(r *http.Request) string {
	cookie, nocookie := r.Cookie("c2i")
	if nocookie != nil || !guestIdValidator.MatchString(cookie.Value) {
		// Generate new guest ID
		f, _ := os.Open("/dev/urandom")
		b := make([]byte, 16)
		f.Read(b)
		f.Close()

		return fmt.Sprintf("%x", b)
	}

	return cookie.Value
}

// Set cookie to store the GuestId
func setGuestId(w http.ResponseWriter, guestId string) {
	http.SetCookie(w, &http.Cookie{
		Name:    "c2i",
		Value:   guestId,
		Path:    "/",
		Expires: time.Now().AddDate(1, 0, 0),
	})
}

// Guest ID Javascript
// Global to all comic IDs, keeps guest ID in browser cache
// Cached aggressively for eternity, handles generating guest IDs
func gidHandler(w http.ResponseWriter, r *http.Request) {
	// Force cache to be maintained
	if len(r.Header["If-None-Match"]) > 0 || len(r.Header["If-Modified-Since"]) > 0 {
		w.WriteHeader(304)
		return
	}

	guestId := getGuestId(r)

	// Cache the JS file as long as possible to maintain guest ID
	w.Header().Set("Cache-Control", "max-age:290304000, private")
	w.Header().Set("Last-Modified", cacheSince)
	w.Header().Set("Expires", cacheUntil)
	w.Header().Set("Content-Type", "text/javascript")
	w.Header().Set("Etag", guestId)

	fmt.Fprintf(w, "gid = '%s'", guestId)
}

// Button image
// Useful for RSS feeds and non-javascript users
// Handles generating and storing guest ID, logs the view
// Cached for one day
func v1ImgHandler(w http.ResponseWriter, r *http.Request) {
	matches := imgComicId.FindStringSubmatch(r.URL.Path)
	if len(matches) != 2 {
		http.NotFound(w, r)
		return
	}

	// Get/set GuestId
	guestId := getGuestId(r)
	setGuestId(w, guestId)

	// Queue the GuestId / ComicID pair to be logged
	view := &View{
		ComicId: matches[1],
		GuestId: guestId,
	}
	viewQueue <- view

	// Cache to restrict to one request a day
	w.Header().Set("Cache-Control", "max-age:86400, private")
	w.Header().Set("Expires", time.Now().AddDate(0, 0, 1).Format(http.TimeFormat))
	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(oldButtons["jpg"])

	w.Header().Set("Connection", "close")
}

// Button Javascript
// Run on comics' sites
// Injects iframe element in to the comic site
// Cached for one day in case it changes
func v1JsHandler(w http.ResponseWriter, r *http.Request) {
	matches := jsComicId.FindStringSubmatch(r.URL.Path)
	if len(matches) != 2 {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "max-age:86400, public")
	w.Header().Set("Expires", time.Now().AddDate(0, 0, 1).Format(http.TimeFormat))
	w.Header().Set("Content-Type", "text/javascript")

	fmt.Fprintf(w, "var a = document.getElementById('comicrank_button'); var i = document.createElement('iframe'); i.src = 'http://stats.comicrank.com/v1/html/%s'; i.width = '88px'; i.height = '31px'; i.style.border = 'none 0'; a.appendChild(i);", matches[1])
}

// Button HTML
// First-pass: Pulls global ID from javascript and redirects. Cached for eternity
// Second-pass: Stores guest ID, logs view. Cached for a day
func v1HtmlHandler(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Path) == 13 {
		matches := htmlComicId.FindStringSubmatch(r.URL.Path)
		if len(matches) != 2 {
			http.NotFound(w, r)
			return
		}

		// Cache GuestId wrapper
		w.Header().Set("Cache-Control", "max-age:290304000, public")
		w.Header().Set("Last-Modified", cacheSince)
		w.Header().Set("Expires", cacheUntil)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		fmt.Fprintf(w, "<script src='/gid.js'></script><body style='background:transparent'><script>location.replace('/v1/html/%s'+gid)</script>", matches[1])

	} else {
		matches := viewIdFinder.FindStringSubmatch(r.URL.Path)
		if len(matches) != 3 {
			http.NotFound(w, r)
			return
		}

		// Refresh guest ID cookie
		setGuestId(w, matches[2])

		// Queue the GuestId / ComicID pair to be logged
		view := &View{
			ComicId: matches[1],
			GuestId: matches[2],
		}
		viewQueue <- view

		// Cache to restrict to one request a day
		w.Header().Set("Cache-Control", "max-age:86400, private")
		w.Header().Set("Expires", time.Now().AddDate(0, 0, 1).Format(http.TimeFormat))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		fmt.Fprintf(w, "<body style='margin:0;padding:0;overflow:hidden'><a href='http://www.comicrank.com/comic/%s/in' target='_blank'><img src='/v1/img.jpg' style='border: none'></a>", view.ComicId)

		w.Header().Set("Connection", "close")
	}
}

// Static image for button HTML
// Cached for eternity
func v1StaticHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "max-age:290304000, public")
	w.Header().Set("Last-Modified", cacheSince)
	w.Header().Set("Expires", cacheUntil)
	w.Header().Set("Content-Type", "image/jpeg")

	w.Write(oldButtons["jpg"])

	w.Header().Set("Connection", "close")
}

// Other site pages
func baseHandler(w http.ResponseWriter, r *http.Request) {
	slices := oldButtonFormat.FindStringSubmatch(r.URL.Path)
	if len(slices) == 4 {
		// Display old images
		w.Header().Set("Cache-Control", "max-age:290304000, public")
		w.Header().Set("Last-Modified", cacheSince)
		w.Header().Set("Expires", cacheUntil)

		switch slices[3] {
		case "gif":
			w.Header().Set("Content-Type", "image/gif")
			break
		case "png":
			w.Header().Set("Content-Type", "image/png")
			break
		case "jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			break
		}
		w.Write(oldButtons[slices[3]])

		w.Header().Set("Connection", "close")

	} else {
		// Display standard paths
		switch r.URL.Path {
		case "/":
			http.Redirect(w, r, "http://www.comicrank.com", 302)
			w.Header().Set("Connection", "close")
			break
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "User-agent: *\nDisallow: /")
			w.Header().Set("Connection", "close")
			break
		default:
			http.NotFound(w, r)
			break
		}
	}
}

// Map to hold old button images in memory
var oldButtons map[string][]byte

// Cache given button image's data in memory
func initImg(index string, filename string) {
	file, _ := os.Open(filename)
	info, _ := file.Stat()
	oldButtons[index] = make([]byte, info.Size())
	file.Read(oldButtons[index])
	file.Close()
}

func main() {
	go viewLogger()

	// Cache old buttons in memory
	oldButtons = make(map[string][]byte, 3)
	initImg("gif", "res/old.gif")
	initImg("jpg", "res/old.jpg")
	initImg("png", "res/old.png")

	// Version-less
	http.HandleFunc("/gid.js", gidHandler)

	// Version 1
	http.HandleFunc("/v1/img/", v1ImgHandler)
	http.HandleFunc("/v1/js/", v1JsHandler)
	http.HandleFunc("/v1/html/", v1HtmlHandler)
	http.HandleFunc("/v1/img.jpg", v1StaticHandler)

	// Start the http server
	http.HandleFunc("/", baseHandler)
	srv := &http.Server{
		Addr:        ":8080",
		ReadTimeout: 15 * time.Second,
	}
	srv.ListenAndServe()
}
