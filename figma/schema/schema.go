package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type User struct {
	ID     string  `json:"id" sokel:"id" label:"User ID"`
	Handle string  `json:"handle" sokel:"handle" label:"Handle"`
	ImgURL *string `json:"img_url,omitempty" sokel:"img_url,optional" label:"Avatar URL"`
}
type FileMetadata struct {
	Name          string  `json:"name" sokel:"name" label:"Name"`
	FolderName    *string `json:"folder_name,omitempty" sokel:"folder_name,optional" label:"Folder name"`
	LastTouchedAt string  `json:"last_touched_at" sokel:"last_touched_at" label:"Last modified"`
	Creator       User    `json:"creator" sokel:"creator" label:"Creator"`
	EditorType    string  `json:"editor_type" sokel:"editor_type" label:"Editor type"`
	Version       *string `json:"version,omitempty" sokel:"version,optional" label:"Version"`
	URL           *string `json:"url,omitempty" sokel:"url,optional" label:"File URL"`
	ThumbnailURL  *string `json:"thumbnail_url,omitempty" sokel:"thumbnail_url,optional" label:"Thumbnail URL"`
	Role          *string `json:"role,omitempty" sokel:"role,optional" label:"Role"`
	LinkAccess    *string `json:"link_access,omitempty" sokel:"link_access,optional" label:"Link access"`
}
type NodeSummary struct {
	ID         string  `json:"id" sokel:"id" label:"Node ID"`
	Name       string  `json:"name" sokel:"name" label:"Name"`
	Type       string  `json:"type" sokel:"type" label:"Node type"`
	Visible    *bool   `json:"visible,omitempty" sokel:"visible,optional" label:"Visible"`
	Characters *string `json:"characters,omitempty" sokel:"characters,optional" label:"Text content"`
}
type NodeResult struct {
	NodeID   string        `json:"node_id" sokel:"node_id" label:"Requested node ID"`
	Found    bool          `json:"found" sokel:"found" label:"Found"`
	Node     *NodeSummary  `json:"node,omitempty" sokel:"node,optional" label:"Node"`
	Children []NodeSummary `json:"children" sokel:"children" label:"Direct children"`
}
type Image struct {
	NodeID   string  `json:"node_id" sokel:"node_id" label:"Node ID"`
	URL      *string `json:"url,omitempty" sokel:"url,optional" label:"Image URL"`
	Rendered bool    `json:"rendered" sokel:"rendered" label:"Rendered"`
}
type Folder struct {
	ID             string  `json:"id" sokel:"id" label:"Folder ID"`
	Name           string  `json:"name" sokel:"name" label:"Name"`
	ParentFolderID *string `json:"parent_folder_id,omitempty" sokel:"parent_folder_id,optional" label:"Parent folder ID"`
}
type File struct {
	Key          string  `json:"key" sokel:"key" label:"File key"`
	Name         string  `json:"name" sokel:"name" label:"Name"`
	ThumbnailURL *string `json:"thumbnail_url,omitempty" sokel:"thumbnail_url,optional" label:"Thumbnail URL"`
	LastModified string  `json:"last_modified" sokel:"last_modified" label:"Last modified"`
}
type Comment struct {
	ID         string  `json:"id" sokel:"id" label:"Comment ID"`
	Message    string  `json:"message" sokel:"message" label:"Comment text"`
	CreatedAt  string  `json:"created_at" sokel:"created_at" label:"Created at"`
	ResolvedAt *string `json:"resolved_at,omitempty" sokel:"resolved_at,optional" label:"Resolved at"`
	ParentID   *string `json:"parent_id,omitempty" sokel:"parent_id,optional" label:"Parent comment ID"`
	User       User    `json:"user" sokel:"user" label:"Author"`
}
type ImageOptions struct {
	Scale             *float64 `json:"scale,omitempty" sokel:"scale,optional" label:"Image scale"`
	ContentsOnly      *bool    `json:"contents_only,omitempty" sokel:"contents_only,optional" label:"Contents only"`
	UseAbsoluteBounds *bool    `json:"use_absolute_bounds,omitempty" sokel:"use_absolute_bounds,optional" label:"Use absolute bounds"`
}
type GetFileMetadata struct{}

func (GetFileMetadata) Meta() contract.Meta {
	return contract.Meta{ID: "get_file_metadata", Label: "Get file metadata", TimeoutSec: 60}
}
func (GetFileMetadata) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_key").Label("File key")}
}
func (GetFileMetadata) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("file", FileMetadata{}).Label("File metadata")}
}

type GetNodes struct{}

func (GetNodes) Meta() contract.Meta {
	return contract.Meta{ID: "get_nodes", Label: "Get node summaries", TimeoutSec: 60}
}
func (GetNodes) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_key").Label("File key"), field.Strings("node_ids").Label("Node IDs"), field.String("version").Label("Version").Optional()}
}
func (GetNodes) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_name").Label("File name"), field.Array("nodes", []NodeResult{}).Label("Nodes")}
}

type RenderImages struct{}

func (RenderImages) Meta() contract.Meta {
	return contract.Meta{ID: "render_images", Label: "Render images", TimeoutSec: 60}
}
func (RenderImages) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_key").Label("File key"), field.Strings("node_ids").Label("Node IDs"), field.String("format").Label("Image format").Optional(), field.Json("options", ImageOptions{}).Label("Image options").Optional()}
}
func (RenderImages) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("images", []Image{}).Label("Images")}
}

type ListTeamFolders struct{}

func (ListTeamFolders) Meta() contract.Meta {
	return contract.Meta{ID: "list_team_folders", Label: "List team folders", TimeoutSec: 60}
}
func (ListTeamFolders) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("team_id").Label("Team ID")}
}
func (ListTeamFolders) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("name").Label("Team name"), field.Array("folders", []Folder{}).Label("Folders")}
}

type ListSubfolders struct{}

func (ListSubfolders) Meta() contract.Meta {
	return contract.Meta{ID: "list_subfolders", Label: "List subfolders", TimeoutSec: 60}
}
func (ListSubfolders) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("folder_id").Label("Folder ID")}
}
func (ListSubfolders) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("name").Label("Folder name"), field.Array("folders", []Folder{}).Label("Folders")}
}

type ListFolderFiles struct{}

func (ListFolderFiles) Meta() contract.Meta {
	return contract.Meta{ID: "list_folder_files", Label: "List folder files", TimeoutSec: 60}
}
func (ListFolderFiles) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("folder_id").Label("Folder ID")}
}
func (ListFolderFiles) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("name").Label("Folder name"), field.Array("files", []File{}).Label("Files")}
}

type ListComments struct{}

func (ListComments) Meta() contract.Meta {
	return contract.Meta{ID: "list_comments", Label: "List comments", TimeoutSec: 60}
}
func (ListComments) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_key").Label("File key")}
}
func (ListComments) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("comments", []Comment{}).Label("Comments")}
}

type CreateComment struct{}

func (CreateComment) Meta() contract.Meta {
	return contract.Meta{ID: "create_comment", Label: "Create comment or reply", TimeoutSec: 60}
}
func (CreateComment) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_key").Label("File key"), field.String("message").Label("Comment text"), field.String("reply_to").Label("Reply to comment ID").Optional()}
}
func (CreateComment) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("comment", Comment{}).Label("Comment")}
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Personal access token")}
}
