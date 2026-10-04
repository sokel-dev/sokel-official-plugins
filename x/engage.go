package main

// 互动：赞 / 转推 / 书签 / 关注，各带一个取消。
//
// 这四对全部挂在**授权账号自己**身上（路径里的 :id 是 me 的 id，不是被操作对象的），
// 这是 X v2 的形状，也是这些操作只能代表授权者本人做的原因。

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// engageOn：POST /2/users/:me/<kind> {tweet_id}
func engageOn(ctx plugin.Ctx, kind, postID, field string) error {
	id := strings.TrimSpace(postID)
	if id == "" {
		return fmt.Errorf("推文 id 是空的")
	}
	u, err := me(ctx)
	if err != nil {
		return err
	}
	return callAPI(ctx, reqOpts{method: http.MethodPost, path: "/users/" + u.ID + "/" + kind,
		body: map[string]any{field: id}}, nil)
}

// engageOff：DELETE /2/users/:me/<kind>/<postID>
func engageOff(ctx plugin.Ctx, kind, postID string) error {
	id := strings.TrimSpace(postID)
	if id == "" {
		return fmt.Errorf("推文 id 是空的")
	}
	u, err := me(ctx)
	if err != nil {
		return err
	}
	return callAPI(ctx, reqOpts{method: http.MethodDelete, path: "/users/" + u.ID + "/" + kind + "/" + id}, nil)
}

func opLike(ctx plugin.Ctx, in *XLikeIn) (*XLikeOut, error) {
	if err := engageOn(ctx, "likes", in.PostID, "tweet_id"); err != nil {
		return nil, err
	}
	return &XLikeOut{OK: true}, nil
}

func opUnlike(ctx plugin.Ctx, in *XUnlikeIn) (*XUnlikeOut, error) {
	if err := engageOff(ctx, "likes", in.PostID); err != nil {
		return nil, err
	}
	return &XUnlikeOut{OK: true}, nil
}

func opRepost(ctx plugin.Ctx, in *XRepostIn) (*XRepostOut, error) {
	if err := engageOn(ctx, "retweets", in.PostID, "tweet_id"); err != nil {
		return nil, err
	}
	return &XRepostOut{OK: true}, nil
}

func opUnrepost(ctx plugin.Ctx, in *XUnrepostIn) (*XUnrepostOut, error) {
	if err := engageOff(ctx, "retweets", in.PostID); err != nil {
		return nil, err
	}
	return &XUnrepostOut{OK: true}, nil
}

func opBookmark(ctx plugin.Ctx, in *XBookmarkIn) (*XBookmarkOut, error) {
	if err := engageOn(ctx, "bookmarks", in.PostID, "tweet_id"); err != nil {
		return nil, err
	}
	return &XBookmarkOut{OK: true}, nil
}

func opUnbookmark(ctx plugin.Ctx, in *XUnbookmarkIn) (*XUnbookmarkOut, error) {
	if err := engageOff(ctx, "bookmarks", in.PostID); err != nil {
		return nil, err
	}
	return &XUnbookmarkOut{OK: true}, nil
}

func opFollow(ctx plugin.Ctx, in *XFollowIn) (*XFollowOut, error) {
	target, err := resolveUserID(ctx, in.UserID, in.Username)
	if err != nil {
		return nil, err
	}
	u, err := me(ctx)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Following     bool `json:"following"`
			PendingFollow bool `json:"pending_follow"`
		} `json:"data"`
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: "/users/" + u.ID + "/following",
		body: map[string]any{"target_user_id": target}}, &resp); err != nil {
		return nil, err
	}
	return &XFollowOut{OK: resp.Data.Following, Pending: resp.Data.PendingFollow}, nil
}

func opUnfollow(ctx plugin.Ctx, in *XUnfollowIn) (*XUnfollowOut, error) {
	target, err := resolveUserID(ctx, in.UserID, in.Username)
	if err != nil {
		return nil, err
	}
	u, err := me(ctx)
	if err != nil {
		return nil, err
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodDelete,
		path: "/users/" + u.ID + "/following/" + target}, nil); err != nil {
		return nil, err
	}
	return &XUnfollowOut{OK: true}, nil
}

// —— 列表成员 ——

func opListMemberAdd(ctx plugin.Ctx, in *XListMemberAddIn) (*XListMemberAddOut, error) {
	listID, uid, err := listTarget(ctx, in.ListID, in.UserID, in.Username)
	if err != nil {
		return nil, err
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: "/lists/" + listID + "/members",
		body: map[string]any{"user_id": uid}}, nil); err != nil {
		return nil, err
	}
	return &XListMemberAddOut{OK: true}, nil
}

func opListMemberRemove(ctx plugin.Ctx, in *XListMemberRemoveIn) (*XListMemberRemoveOut, error) {
	listID, uid, err := listTarget(ctx, in.ListID, in.UserID, in.Username)
	if err != nil {
		return nil, err
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodDelete,
		path: "/lists/" + listID + "/members/" + uid}, nil); err != nil {
		return nil, err
	}
	return &XListMemberRemoveOut{OK: true}, nil
}

func listTarget(ctx plugin.Ctx, listID, userID, username string) (string, string, error) {
	id := strings.TrimSpace(listID)
	if id == "" {
		return "", "", fmt.Errorf("列表 id 是空的")
	}
	uid, err := resolveUserID(ctx, userID, username)
	if err != nil {
		return "", "", err
	}
	return id, uid, nil
}
