package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type TemplateCardUpdate struct {
	InvalidUsers []string `json:"invalid_users"`
}

// UpdateTemplateCard implements document/path/94888. Each response_code can be
// consumed once within 72 hours. Only callback-capable original template cards
// are updateable. Never retry an ambiguous network result with the same code.
// Explicit member IDs prevent JSON data from widening an update to all members.
func (c *WeComClient) UpdateTemplateCard(ctx context.Context, cred Cred, users []string, responseCode string, card json.RawMessage) (TemplateCardUpdate, error) {
	var result TemplateCardUpdate
	cred, agentID, err := normalizedWeComCred(cred)
	if err != nil {
		return result, err
	}
	if _, err := weComRecipients(users); err != nil {
		return result, err
	}
	if strings.TrimSpace(responseCode) == "" || !utf8.ValidString(responseCode) || len(responseCode) > 4096 {
		return result, errors.New("response_code 不能为空，且不能超过 4096 字节；请使用 72 小时内且未消费的 code")
	}
	object, err := apiObject(card)
	if err != nil {
		return result, err
	}
	if err := apiAllowedFields(object, "button", "template_card", "enable_id_trans"); err != nil {
		return result, err
	}
	button, hasButton := object["button"]
	template, hasTemplate := object["template_card"]
	if hasButton == hasTemplate {
		return result, errors.New("更新内容必须且只能包含 button 或 template_card 中的一种")
	}
	if hasButton {
		replacement, err := apiObject(button)
		if err != nil {
			return result, err
		}
		if err := apiAllowedFields(replacement, "replace_name"); err != nil {
			return result, err
		}
		name, err := apiString(replacement["replace_name"])
		if err != nil || strings.TrimSpace(name) == "" {
			return result, errors.New("button.replace_name 必须是非空字符串")
		}
		if _, exists := object["enable_id_trans"]; exists {
			return result, errors.New("enable_id_trans 仅用于完整 template_card 更新")
		}
	} else {
		content, err := apiObject(template)
		if err != nil {
			return result, err
		}
		kind, err := apiString(content["card_type"])
		if err != nil {
			return result, errors.New("template_card 必须包含 card_type")
		}
		switch kind {
		case "text_notice", "news_notice", "button_interaction", "vote_interaction", "multiple_interaction":
		default:
			return result, errors.New("不支持的模板卡片 card_type")
		}
		if flag, exists := object["enable_id_trans"]; exists {
			if err := apiInt(flag, 0, 1); err != nil {
				return result, err
			}
		}
	}
	userIDs, _ := json.Marshal(users)
	object["userids"] = userIDs
	object["agentid"] = json.RawMessage(strconv.FormatInt(agentID, 10))
	object["response_code"] = apiRawString(responseCode)
	body, _ := json.Marshal(object)
	b, err := c.authenticated(ctx, cred, http.MethodPost, "/cgi-bin/message/update_template_card", nil, body)
	if err != nil {
		return result, err
	}
	var out struct {
		InvalidUsers []string `json:"invaliduser"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return result, errors.New("企业微信更新卡片响应字段格式错误；部分成员可能已更新，请先核实")
	}
	result.InvalidUsers = out.InvalidUsers
	if result.InvalidUsers == nil {
		result.InvalidUsers = []string{}
	}
	return result, nil
}
