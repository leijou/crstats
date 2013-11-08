package main

import (
	"fmt"
	"testing"
)

var host = "127.0.0.1:6379"

func TestConnect(t *testing.T) {
	client := &StatsClient{}
	err := client.Connect(host)
	if err != nil {
		t.Error(err)
	}
}

func TestAddView(t *testing.T) {
	client := &StatsClient{}
	err := client.Connect(host)
	if err != nil {
		t.Error(err)
	}

	err = client.AddView("TEST_comicid", "TEST_guestid")
	if err != nil {
		t.Error(err)
	}
}

func TestFetchComicStats(t *testing.T) {
	client := &StatsClient{}
	err := client.Connect(host)
	if err != nil {
		t.Error(err)
	}

	stats, err := client.FetchComicStats("TEST_comicid")
	fmt.Println(stats)
	if err != nil {
		t.Error(err)
	}
}
