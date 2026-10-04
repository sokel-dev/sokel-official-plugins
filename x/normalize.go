package main

// 归一化：X 的应答 → 契约里的 Post / User。
//
// X 把作者、媒体、被引用的推文都堆在 includes 里，靠 expansions 与下标关联。
// 画布上没法引用「includes.users 里 id 等于 author_id 的那个」——所以关联在这里做完，
// 出去的每条推文都自带 author_username 与展开后的媒体。

import (
	"encoding/json"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/x/schema"
)

// rawPost：X 的推文对象（只取用得上的字段）。
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

// includes：应答里的展开区。
type includes struct {
	Users  []rawUser  `json:"users"`
	Media  []rawMedia `json:"media"`
	Tweets []rawPost  `json:"tweets"`
}

// listEnvelope：所有列表型读接口的共同外壳。
type listEnvelope struct {
	Data     []rawPost `json:"data"`
	Includes includes  `json:"includes"`
	Meta     struct {
		ResultCount int    `json:"result_count"`
		NewestID    string `json:"newest_id"`
		OldestID    string `json:"oldest_id"`
		NextToken   string `json:"next_token"`
	} `json:"meta"`
	// Errors：X 会在 200 里带上「有几条取不到」（删了/私密）。**不是错误**，
	// 当错误处理的话，一批里混进一条删掉的推文就会让整个节点失败。
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

// toPost：一条推文 → 契约形状。
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
	// 类型：X 只给一个 referenced_tweets 数组，第一项就是这条推文之于别人的关系。
	if len(p.ReferencedTweets) > 0 {
		out.Kind = p.ReferencedTweets[0].Type
		out.RefPostID = p.ReferencedTweets[0].ID
	}
	for _, e := range p.Entities.URLs {
		// 展开后的地址才有用；X 自己的 t.co 短链下游打不开也读不懂。
		// 引用/转推会带一条指向被引推文的链接，那个不是「正文里的外链」，去掉。
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
			url = m.PreviewImageURL // 视频/GIF 在这个接口只给预览图
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

// postURL：推文链接。X 用 /i/status/<id> 也能打开，所以作者未知时不至于给不出链接。
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
