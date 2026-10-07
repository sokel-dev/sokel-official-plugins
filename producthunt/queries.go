package main

// GraphQL queries. Every field here is checked against Product Hunt's published schema (testdata/schema.graphql)
// by TestQueriesMatchSchema: the API refuses even introspection without a token, so a misspelt field would
// otherwise only surface on a user's first call.

// isViewer is the only identity Product Hunt gives away on a comment: id / username / name / url come back as
// "0" / "[REDACTED]" to API clients (measured 2026-10-07, makers and the viewer excepted). So "my comment" and
// "reply to me" go through isViewer, username is kept in case Product Hunt ever returns it, and the other user
// fields are not asked for: each one counts towards the 500 000 complexity cap (with all four, 20 × 5 replies
// came to 526 011 and was refused).
const userFields = `username isViewer`

const commentFields = `id body url votesCount parentId createdAt user { ` + userFields + ` }`

const postFields = `id slug name tagline description url website votesCount commentsCount createdAt featuredAt makers { username }`

const qPost = `query Post($slug: String!) {
  post(slug: $slug) { ` + postFields + ` }
}`

// pageSize is what Product Hunt hands back per page whatever `first` says (first: 50 returned 20 with
// hasNextPage, measured 2026-10-07); anything beyond it is paged with the cursor.
const pageSize = 20

// repliesPerComment keeps the comments query under Product Hunt's complexity cap (500 000 per query): a page of
// 20 comments costs about 20 × (replies × 4 800 + 150), so 5 replies fit (≈ 484 000) and 10 do not (measured).
// Product Hunt threads are almost always one reply deep and rarely have more than a few replies.
const repliesPerComment = 5

const pageInfoFields = `pageInfo { endCursor hasNextPage }`

// qComments: one page of top-level comments newest first, each with its newest replies.
const qComments = `query Comments($slug: String!, $first: Int!, $after: String) {
  post(slug: $slug) {
    id name url commentsCount
    comments(first: $first, after: $after, order: NEWEST) {
      totalCount ` + pageInfoFields + `
      edges { node { ` + commentFields + `
        replies(first: 5, order: NEWEST) { edges { node { ` + commentFields + ` } } }
      } }
    }
  }
}`

const qLeaderboard = `query Leaderboard($after: DateTime!, $before: DateTime!, $order: PostsOrder!, $first: Int!, $cursor: String) {
  posts(postedAfter: $after, postedBefore: $before, order: $order, first: $first, after: $cursor) {
    ` + pageInfoFields + `
    edges { node { ` + postFields + ` } }
  }
}`

const qViewer = `query Viewer {
  viewer { user { id username madePosts(first: 10) { edges { node { slug } } } } }
}`
