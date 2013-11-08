package main

import (
	"errors"
	"github.com/fzzy/radix/redis"
	"time"
)

type ComicStats struct {
	ComicId     string
	LastSeen    time.Time
	Readers     int
	Readers24h  int
	Visitors24h int
}

var CommunicationError error = errors.New("communication with redis failed")
var ComicNotFoundError error = errors.New("comic not found in redis")

// StatsClient manages the I/O of the statistics database
type StatsClient struct {
	connection *redis.Client
	host       string
	LastError  error
}

// errorHandler logs errors, returning a generic communication to the caller
// A test ping to redis will be made, connection will be re-opened if it fails
func (client *StatsClient) errorHandler(err error) error {
	client.LastError = err

	r := client.connection.Cmd("PING")
	if r.Err != nil {
		client.reconnect()
	}

	return CommunicationError
}

// Connect creates a connection to the given Redis server
func (client *StatsClient) Connect(host string) error {
	client.host = host

	err := client.reconnect()
	if err != nil {
		return client.errorHandler(err)
	}

	return nil
}

// reconnect
func (client *StatsClient) reconnect() (err error) {
	if client.connection != nil {
		client.connection.Close()
		client.connection = nil
	}

	client.connection, err = redis.DialTimeout("tcp", client.host, time.Duration(2)*time.Second)
	if err != nil {
		return
	}

	// Set flush to disk once every 15 minutes regardless of writes
	client.connection.Cmd("CONFIG", "SET", "save", "900 1")

	return
}

// AddView logs a pageview in the database
// Will discard duplicate views
// Performs necessary processing for future stats collection
func (client *StatsClient) AddView(comicId string, guestId string) error {
	var r *redis.Reply

	// Rate limiter (one incr per comic per visitor per day)
	r = client.connection.Cmd("SET", "guest-"+comicId+"-"+guestId, 1, "NX", "EX", 60*60*24)
	if r.Type == redis.NilReply {
		return nil
	}
	if r.Err != nil {
		return client.errorHandler(r.Err)
	}

	// Record comic last seen
	client.connection.Cmd("ZADD", "comics", time.Now().Unix(), comicId)

	// Increment & return visited days count
	r = client.connection.Cmd("INCR", "visitor-"+comicId+"-"+guestId)
	daysVisited, err := r.Int()

	// Auto-expire visitor after 14 days
	client.connection.Cmd("EXPIRE", "visitor-"+comicId+"-"+guestId, 60*60*24*14)

	if err != nil {
		return client.errorHandler(err)
	}

	if daysVisited <= 2 {
		// Add visitor to the 24 hour visitor list
		client.connection.Cmd("ZADD", "visitors-daily-"+comicId, time.Now().Unix(), guestId)
	} else {
		// Add/Reset expiry of reader on the comic
		client.connection.Cmd("ZADD", "readers-"+comicId, time.Now().Unix(), guestId)
	}

	return nil
}

// FetchComicStats creates, populates, and returns a ComicStats object for the given comic
func (client *StatsClient) FetchComicStats(comicId string) (stats *ComicStats, err error) {
	stats = &ComicStats{ComicId: comicId}

	// Get comic last seen
	r := client.connection.Cmd("ZSCORE", "comics", comicId)
	if r.Type == redis.NilReply {
		err = ComicNotFoundError
		return
	}
	if r.Err != nil {
		err = client.errorHandler(err)
		return
	}

	// Parse last seen time
	t, err := r.Int64()
	if err != nil {
		return
	}
	stats.LastSeen = time.Unix(t, 0)

	// Prune comic details
	client.connection.Cmd("ZREMRANGEBYSCORE", "readers-"+comicId, "-inf", (time.Now().Unix() - 60*60*24*14))
	client.connection.Cmd("ZREMRANGEBYSCORE", "visitors-daily-"+comicId, "-inf", (time.Now().Unix() - 60*60*24))

	// Get reader count
	r = client.connection.Cmd("ZCARD", "readers-"+comicId)
	stats.Readers, _ = r.Int()

	// Get 24h reader count
	r = client.connection.Cmd("ZCOUNT", "readers-"+comicId, (time.Now().Unix() - 60*60*24), "+inf")
	stats.Readers24h, _ = r.Int()

	// Get 24h visitor count
	r = client.connection.Cmd("ZCARD", "visitors-daily-"+comicId)
	stats.Visitors24h, _ = r.Int()

	return
}
