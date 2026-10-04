package main

// Normalization: X's response -> the contract's Post / User.
//
// X dumps authors, media, and quoted tweets into includes, linked by expansions and index. There's
// no way to reference "the one in includes.users whose id equals author_id" on the canvas — so the
// linking is done entirely here, and every outgoing tweet carries its own author_username and
// expanded media.

import (
	"encoding/json"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/x/schema"
)

// rawPost is X's tweet object (only the fields actually used).
type rawPost struct {
	ID             string `json:"id"`
	Text           string `json:"text"`
	CreatedAt      string `json:"created_at"`
	AuthorID       string `json:"author_id"`
	ConversationID string `json:"conversation_id"`
	Lang           string `json:"lang"`
	PublicMetrics  struct {
		ReplyCount      int `json:"reply_count"`
		RetweetCount    int `json:"retweet_count"`
		LikeCount       int `json:"like_count"`
		QuoteCount      int `json:"quote_count"`
		ImpressionCount int `json:"impression_count"`
	} `json:"public_metrics"`
	ReferencedTweets []struct {
		Type string `json:"type"` // retweeted / quoted / replied_to
		ID   string `json:"id"`
	} `json:"referenced_tweets"`
	Entities struct {
		URLs []struct {
			ExpandedURL string `json:"expanded_url"`
			URL         string `json:"url"`
		} `json:"urls"`
		Hashtags []struct {
			Tag string `json:"tag"`
		} `json:"hashtags"`
	} `json:"entities"`
	Attachments struct {
		MediaKeys []string `json:"media_keys"`
	} `json:"attachments"`
}

type rawUser struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Username      string `json:"username"`
	Description   string `json:"description"`
	URL           string `json:"url"`
	Verified      bool   `json:"verified"`
	Protected     bool   `json:"protected"`
	CreatedAt     string `json:"created_at"`
	PublicMetrics struct {
		FollowersCount int `json:"followers_count"`
		FollowingCount int `json:"following_count"`
		TweetCount     int `json:"tweet_count"`
	} `json:"public_metrics"`
}

type rawMedia struct {
	MediaKey        string `json:"media_key"`
	Type            string `json:"type"`
	URL             string `json:"url"`
	PreviewImageURL string `json:"preview_image_url"`
	AltText         string `json:"alt_text"`
}

// includes is the expansion section of a response.
type includes struct {
	Users  []rawUser  `json:"users"`
	Media  []rawMedia `json:"media"`
	Tweets []rawPost  `json:"tweets"`
}

// listEnvelope is the common envelope for all list-style read endpoints.
type listEnvelope struct {
	Data     []rawPost `json:"data"`
	Includes includes  `json:"includes"`
	Meta     struct {
		ResultCount int    `json:"result_count"`
		NewestID    string `json:"newest_id"`
		OldestID    string `json:"oldest_id"`
		NextToken   string `json:"next_token"`
	} `json:"meta"`
	// Errors: X includes "a few items couldn't be fetched" (deleted/protected) inside a 200
	// response. **This is not an error** — treating it as one would fail the whole node just
	// because one deleted tweet showed up mixed into a batch.
	Errors []json.RawMessage `json:"errors"`
}

func (inc includes) userByID(id string) rawUser {
	for _, u := range inc.Users {
		if u.ID == id {
			return u
		}
	}
	return rawUser{}
}

func (inc includes) mediaByKey(key string) rawMedia {
	for _, m := range inc.Media {
		if m.MediaKey == key {
			return m
		}
	}
	return rawMedia{}
}

// toPost converts one tweet to the contract shape.
func toPost(p rawPost, inc includes) schema.Post {
	u := inc.userByID(p.AuthorID)
	out := schema.Post{
		ID: p.ID, Text: p.Text, CreatedAt: p.CreatedAt,
		AuthorID: p.AuthorID, AuthorUsername: u.Username, AuthorName: u.Name,
		ConversationID: p.ConversationID, Lang: p.Lang,
		ReplyCount:  p.PublicMetrics.ReplyCount,
		RepostCount: p.PublicMetrics.RetweetCount,
		LikeCount:   p.PublicMetrics.LikeCount,
		QuoteCount:  p.PublicMetrics.QuoteCount,
		ViewCount:   p.PublicMetrics.ImpressionCount,
		Kind:        "original",
	}
	out.URL = postURL(u.Username, p.ID)
	// Kind: X only gives a referenced_tweets array; the first entry is this tweet's relationship
	// to another one.
	if len(p.ReferencedTweets) > 0 {
		out.Kind = p.ReferencedTweets[0].Type
		out.RefPostID = p.ReferencedTweets[0].ID
	}
	for _, e := range p.Entities.URLs {
		// Only the expanded address is useful; X's own t.co short links can't be opened or
		// understood downstream. A quote/retweet carries a link pointing back at the quoted
		// tweet — that's not "a link in the body", so it's dropped.
		if e.ExpandedURL == "" || strings.Contains(e.ExpandedURL, "twitter.com/i/web/status") {
			continue
		}
		out.Links = append(out.Links, e.ExpandedURL)
	}
	for _, h := range p.Entities.Hashtags {
		out.Tags = append(out.Tags, h.Tag)
	}
	for _, k := range p.Attachments.MediaKeys {
		m := inc.mediaByKey(k)
		if m.MediaKey == "" {
			continue
		}
		url := m.URL
		if url == "" {
			url = m.PreviewImageURL // This endpoint only gives a preview image for video/GIF
		}
		out.Media = append(out.Media, schema.Media{
			MediaKey: m.MediaKey, Type: m.Type, URL: url, AltText: m.AltText,
		})
	}
	return out
}

func toPosts(list listEnvelope) []schema.Post {
	items := make([]schema.Post, 0, len(list.Data))
	for _, p := range list.Data {
		items = append(items, toPost(p, list.Includes))
	}
	return items
}

func toUser(u rawUser) schema.User {
	return schema.User{
		ID: u.ID, Username: u.Username, Name: u.Name, Description: u.Description,
		URL: profileURL(u.Username), Verified: u.Verified, Protected: u.Protected,
		CreatedAt:      u.CreatedAt,
		FollowersCount: u.PublicMetrics.FollowersCount,
		FollowingCount: u.PublicMetrics.FollowingCount,
		PostCount:      u.PublicMetrics.TweetCount,
	}
}

// postURL builds a tweet link. X's /i/status/<id> form also opens fine, so an unknown author
// doesn't mean there's no link to give.
func postURL(username, id string) string {
	if id == "" {
		return ""
	}
	if username == "" {
		return "https://x.com/i/status/" + id
	}
	return "https://x.com/" + username + "/status/" + id
}

func profileURL(username string) string {
	if username == "" {
		return ""
	}
	return "https://x.com/" + username
}
