package main

// Projects V2 (boards) goes through GraphQL — classic Projects' REST endpoints have been
// retired by GitHub.
//
// The board API's shape is quite different from REST, and there are two conventions worth
// remembering:
//   - Everything needs a **node ID** (starting with PVT_/PVTI_/PVTSSF_), not a number. So every
//     write operation must first look up that ID; we hide that extra round trip inside the
//     implementation and only expose human-readable numbers and names at the contract level.
//   - **Organizations and individuals are two different queries** (organization{} vs user{}).
//     Guessing wrong doesn't error out — it just returns null data, which looks like "this board
//     is empty". So when is_org is left unset, we try both.

import (
	"fmt"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/github/schema"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// ownerQuery builds the organization/user query shell based on is_org.
// The return value is "which kinds to try" — left unset, both are tried, organization first.
func ownerKinds(isOrg bool, explicit bool) []string {
	if explicit {
		if isOrg {
			return []string{"organization"}
		}
		return []string{"user"}
	}
	return []string{"organization", "user"}
}

func opProjectsList(ctx plugin.Ctx, in *ProjectsListIn) (*ProjectsListOut, error) {
	owner := strings.TrimSpace(in.Owner)
	if owner == "" {
		return nil, fmt.Errorf("要填用户名或组织名——看板挂在账号下，不在仓库下")
	}
	var lastErr error
	for _, kind := range ownerKinds(in.IsOrg, in.IsOrg) {
		q := fmt.Sprintf(`query($login:String!){%s(login:$login){projectsV2(first:50,orderBy:{field:NUMBER,direction:DESC}){nodes{id number title closed url items(first:1){totalCount}}}}}`, kind)
		data, err := ghGraphQL(ctx, q, map[string]any{"login": owner})
		if err != nil {
			lastErr = err
			continue
		}
		root, _ := data[kind].(map[string]any)
		if root == nil {
			continue // this identity doesn't exist, try the other kind
		}
		nodes := arr(obj(root, "projectsV2"), "nodes")
		out := &ProjectsListOut{Count: len(nodes)}
		for _, it := range nodes {
			p, _ := it.(map[string]any)
			if p == nil {
				continue
			}
			out.Projects = append(out.Projects, schema.Project{
				Number: num(p, "number"), ID: str(p, "id"), Title: str(p, "title"),
				Closed: boolean(p, "closed"), URL: str(p, "url"),
				Items: num(obj(p, "items"), "totalCount"),
			})
		}
		return out, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("找不到 %q 的看板——确认这个名字是用户还是组织（组织请把「是组织」打开），"+
		"且令牌有 project 权限", owner)
}

// projectNodeID resolves a board number to its node ID. Every write operation needs it.
func projectNodeID(ctx plugin.Ctx, owner string, number int, isOrg bool) (string, error) {
	var lastErr error
	for _, kind := range ownerKinds(isOrg, isOrg) {
		q := fmt.Sprintf(`query($login:String!,$number:Int!){%s(login:$login){projectV2(number:$number){id}}}`, kind)
		data, err := ghGraphQL(ctx, q, map[string]any{"login": owner, "number": number})
		if err != nil {
			lastErr = err
			continue
		}
		if id := str(obj(obj(data, kind), "projectV2"), "id"); id != "" {
			return id, nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("找不到 %s 下编号为 %d 的看板", owner, number)
}

func opProjectItemsList(ctx plugin.Ctx, in *ProjectItemsListIn) (*ProjectItemsListOut, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100 // GraphQL single-page limit
	}
	var lastErr error
	for _, kind := range ownerKinds(in.IsOrg, in.IsOrg) {
		q := fmt.Sprintf(`query($login:String!,$number:Int!,$first:Int!){%s(login:$login){projectV2(number:$number){
  items(first:$first){pageInfo{hasNextPage} nodes{ id type
    fieldValues(first:20){nodes{
      ... on ProjectV2ItemFieldTextValue{text field{... on ProjectV2FieldCommon{name}}}
      ... on ProjectV2ItemFieldNumberValue{number field{... on ProjectV2FieldCommon{name}}}
      ... on ProjectV2ItemFieldSingleSelectValue{name field{... on ProjectV2FieldCommon{name}}}
      ... on ProjectV2ItemFieldDateValue{date field{... on ProjectV2FieldCommon{name}}}
    }}
    content{
      ... on schema.Issue{number title state url repository{nameWithOwner}}
      ... on PullRequest{number title state url repository{nameWithOwner}}
      ... on DraftIssue{title}
    }}}}}}`, kind)
		data, err := ghGraphQL(ctx, q, map[string]any{
			"login": strings.TrimSpace(in.Owner), "number": in.Number, "first": limit,
		})
		if err != nil {
			lastErr = err
			continue
		}
		proj := obj(obj(data, kind), "projectV2")
		if proj == nil {
			continue
		}
		items := obj(proj, "items")
		out := &ProjectItemsListOut{HasMore: boolean(obj(items, "pageInfo"), "hasNextPage")}
		for _, it := range arr(items, "nodes") {
			n, _ := it.(map[string]any)
			if n == nil {
				continue
			}
			c := obj(n, "content")
			item := schema.ProjectItem{
				ID: str(n, "id"), Type: str(n, "type"), Title: str(c, "title"),
				Number: num(c, "number"), State: str(c, "state"), URL: str(c, "url"),
				Repo: str(obj(c, "repository"), "nameWithOwner"),
			}
			for _, fv := range arr(obj(n, "fieldValues"), "nodes") {
				f, _ := fv.(map[string]any)
				if f == nil {
					continue
				}
				name := str(obj(f, "field"), "name")
				if name == "" {
					continue
				}
				val := firstNonEmpty(str(f, "name"), str(f, "text"), str(f, "date"), str(f, "number"))
				if val == "" {
					continue
				}
				item.Fields = append(item.Fields, name+"="+val)
				if name == "Status" {
					item.Status = val
				}
			}
			out.Items = append(out.Items, item)
		}
		out.Count = len(out.Items)
		return out, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("找不到看板 %s#%d", in.Owner, in.Number)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" && v != "<nil>" {
			return v
		}
	}
	return ""
}

func opProjectItemAdd(ctx plugin.Ctx, in *ProjectItemAddIn) (*ProjectItemAddOut, error) {
	projID, err := projectNodeID(ctx, strings.TrimSpace(in.Owner), in.ProjectNumber, in.IsOrg)
	if err != nil {
		return nil, err
	}
	o, r, err := repoSplit(in.Repo)
	if err != nil {
		return nil, err
	}
	// First get the Issue/PR's node ID. Issue and PR are different GraphQL types, but
	// issueOrPullRequest recognizes both.
	data, err := ghGraphQL(ctx,
		`query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){issueOrPullRequest(number:$number){... on schema.Issue{id} ... on PullRequest{id}}}}`,
		map[string]any{"owner": o, "name": r, "number": in.Number})
	if err != nil {
		return nil, err
	}
	contentID := str(obj(obj(data, "repository"), "issueOrPullRequest"), "id")
	if contentID == "" {
		return nil, fmt.Errorf("%s 里没有编号 %d 的 Issue 或 PR", in.Repo, in.Number)
	}
	// addProjectV2ItemById returns the same card for content already on the board, so it's
	// naturally idempotent.
	res, err := ghGraphQL(ctx,
		`mutation($project:ID!,$content:ID!){addProjectV2ItemById(input:{projectId:$project,contentId:$content}){item{id}}}`,
		map[string]any{"project": projID, "content": contentID})
	if err != nil {
		return nil, err
	}
	return &ProjectItemAddOut{
		ItemID: str(obj(obj(res, "addProjectV2ItemById"), "item"), "id"),
	}, nil
}

// opProjectItemFieldSet changes a card's field (moves it between columns).
//
// Three steps: find the board -> find the field (picking up the single-select option ID along
// the way) -> update it. Both field name and option name are matched **case-insensitively**, but
// when nothing matches, the available values are listed — column names on a board are typed by
// hand, so guessing is pointless.
func opProjectItemFieldSet(ctx plugin.Ctx, in *ProjectItemFieldSetIn) (*ProjectItemFieldSetOut, error) {
	owner := strings.TrimSpace(in.Owner)
	projID, err := projectNodeID(ctx, owner, in.ProjectNumber, in.IsOrg)
	if err != nil {
		return nil, err
	}
	fieldName := orDefault(in.Field, "Status")
	data, err := ghGraphQL(ctx,
		`query($id:ID!){node(id:$id){... on ProjectV2{fields(first:50){nodes{
  ... on ProjectV2FieldCommon{id name dataType}
  ... on ProjectV2SingleSelectField{id name options{id name}}
}}}}}`, map[string]any{"id": projID})
	if err != nil {
		return nil, err
	}
	var fieldID, optionID, dataType string
	var known, options []string
	for _, it := range arr(obj(obj(data, "node"), "fields"), "nodes") {
		f, _ := it.(map[string]any)
		if f == nil {
			continue
		}
		name := str(f, "name")
		if name == "" {
			continue
		}
		known = append(known, name)
		if !strings.EqualFold(name, fieldName) {
			continue
		}
		fieldID, dataType = str(f, "id"), str(f, "dataType")
		for _, oit := range arr(f, "options") {
			o, _ := oit.(map[string]any)
			if o == nil {
				continue
			}
			options = append(options, str(o, "name"))
			if strings.EqualFold(str(o, "name"), in.Value) {
				optionID = str(o, "id")
			}
		}
	}
	if fieldID == "" {
		return nil, fmt.Errorf("看板上没有字段 %q——现有字段：%s", fieldName, strings.Join(known, " / "))
	}
	value := map[string]any{}
	switch {
	case len(options) > 0:
		if optionID == "" {
			return nil, fmt.Errorf("字段 %q 没有 %q 这个选项——可选值：%s",
				fieldName, in.Value, strings.Join(options, " / "))
		}
		value["singleSelectOptionId"] = optionID
	case dataType == "NUMBER":
		var f float64
		if _, err := fmt.Sscanf(in.Value, "%g", &f); err != nil {
			return nil, fmt.Errorf("字段 %q 是数字类型，但给的是 %q", fieldName, in.Value)
		}
		value["number"] = f
	case dataType == "DATE":
		value["date"] = in.Value
	default:
		value["text"] = in.Value
	}
	if _, err := ghGraphQL(ctx,
		`mutation($project:ID!,$item:ID!,$field:ID!,$value:ProjectV2FieldValue!){
  updateProjectV2ItemFieldValue(input:{projectId:$project,itemId:$item,fieldId:$field,value:$value}){projectV2Item{id}}}`,
		map[string]any{"project": projID, "item": in.ItemID, "field": fieldID, "value": value}); err != nil {
		return nil, err
	}
	return &ProjectItemFieldSetOut{Updated: true, Field: fieldName, Value: in.Value}, nil
}
