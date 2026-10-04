package schema

// Element shapes used in operation outputs.
//
// One decision runs through all of it: **normalization is done entirely in the plugin**. X's
// response dumps authors, media, and quoted tweets into `includes`, linked by `expansions` and
// index — referencing `items[0].author_username` on the canvas works, but referencing
// `includes.users[3].username` doesn't (the index shifts with every response). Likewise, links in
// the body are always expanded to their real addresses: a t.co short link is useless downstream,
// and the expanded form is already in the same response.

// Post is a single tweet (X now calls it a "Post", though the UI still calls it "推文"/tweet, as
// everyone's used to).
type Post struct {
	ID        string `sokel:"id" label:"推文 id"`
	Text      string `sokel:"text" label:"正文"`
	CreatedAt string `sokel:"created_at,optional" label:"发布时间"`
	URL       string `sokel:"url,optional" label:"链接" desc:"https://x.com/<作者>/status/<id>，可直接点开"`

	AuthorID       string `sokel:"author_id,optional" label:"作者 id"`
	AuthorUsername string `sokel:"author_username,optional" label:"作者用户名" desc:"不带 @"`
	AuthorName     string `sokel:"author_name,optional" label:"作者昵称"`

	ConversationID string `sokel:"conversation_id,optional" label:"对话 id" desc:"整条回复树/推串的根 id，用它把一串回复归堆"`
	// Kind: X doesn't give us this field; it's derived from referenced_tweets.
	// "Most of a search result is retweets" is something every X automation runs into the first
	// time; without a way to tell them apart, downstream would have no choice but to guess.
	Kind      string `sokel:"kind,optional" label:"类型" desc:"original 原创 / replied_to 回复 / retweeted 转推 / quoted 引用"`
	RefPostID string `sokel:"ref_post_id,optional" label:"被引用的推文 id" desc:"类型非 original 时有值"`
	Lang      string `sokel:"lang,optional" label:"语言"`

	ReplyCount  int `sokel:"reply_count,optional" label:"回复数"`
	RepostCount int `sokel:"repost_count,optional" label:"转推数"`
	LikeCount   int `sokel:"like_count,optional" label:"点赞数"`
	QuoteCount  int `sokel:"quote_count,optional" label:"引用数"`
	ViewCount   int `sokel:"view_count,optional" label:"阅读数" desc:"impression_count；早期推文可能为 0"`

	Media []Media  `sokel:"media,optional" label:"媒体"`
	Links []string `sokel:"links,optional" label:"外链" desc:"已展开成原地址（正文里是 t.co 短链，下游用不了）"`
	Tags  []string `sokel:"tags,optional" label:"话题标签" desc:"不带 #"`
}

// Media is one image/video attached to a tweet.
type Media struct {
	MediaKey string `sokel:"media_key" label:"媒体键"`
	Type     string `sokel:"type,optional" label:"类型" desc:"photo / video / animated_gif"`
	URL      string `sokel:"url,optional" label:"地址" desc:"图是原图；视频给的是预览图——X 在这个接口不给视频源地址"`
	AltText  string `sokel:"alt_text,optional" label:"替代文本"`
}

// User is one account.
type User struct {
	ID          string `sokel:"id" label:"用户 id"`
	Username    string `sokel:"username,optional" label:"用户名" desc:"不带 @"`
	Name        string `sokel:"name,optional" label:"昵称"`
	Description string `sokel:"description,optional" label:"简介"`
	URL         string `sokel:"url,optional" label:"主页地址"`
	Verified    bool   `sokel:"verified,optional" label:"已认证"`
	Protected   bool   `sokel:"protected,optional" label:"私密账号" desc:"它的推文只有粉丝读得到，搜索也搜不出来"`
	CreatedAt   string `sokel:"created_at,optional" label:"注册时间"`

	FollowersCount int `sokel:"followers_count,optional" label:"粉丝数"`
	FollowingCount int `sokel:"following_count,optional" label:"关注数"`
	PostCount      int `sokel:"post_count,optional" label:"推文数"`
}

// DMEvent is a single direct-message event.
type DMEvent struct {
	ID             string `sokel:"id" label:"事件 id"`
	Kind           string `sokel:"kind,optional" label:"类型" desc:"MessageCreate 消息 / ParticipantsJoin 加入 / ParticipantsLeave 退出"`
	ConversationID string `sokel:"conversation_id,optional" label:"会话 id" desc:"回信要用它"`
	SenderID       string `sokel:"sender_id,optional" label:"发送者 id"`
	Text           string `sokel:"text,optional" label:"正文"`
	CreatedAt      string `sokel:"created_at,optional" label:"时间"`
}
