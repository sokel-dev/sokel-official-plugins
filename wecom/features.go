package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

type featureApplication struct {
	client        *WeComClient
	identity      *WeComIdentityClient
	callbacks     *CallbackService
	callbackStore *CallbackStore
}

func (a *featureApplication) register(p *sokel.Plugin) {
	OnSendMessage(p, a.sendMessage)
	OnSendTemplateCard(p, a.sendTemplateCard)
	OnUpdateTemplateCard(p, a.updateTemplateCard)
	OnRecallMessage(p, a.recallMessage)
	OnUploadMedia(p, a.uploadMedia)
	OnGetApplication(p, a.getApplication)
	OnSetApplication(p, a.setApplication)
	OnGetMenu(p, a.getMenu)
	OnSetMenu(p, a.setMenu)
	OnDeleteMenu(p, a.deleteMenu)
	OnGetWorkbenchTemplate(p, a.getWorkbenchTemplate)
	OnSetWorkbenchTemplate(p, a.setWorkbenchTemplate)
	OnGetWorkbenchData(p, a.getWorkbenchData)
	OnSetWorkbenchData(p, a.setWorkbenchData)
	OnBatchSetWorkbenchData(p, a.batchSetWorkbenchData)
	OnJsSdkConfig(p, a.jsSDKConfig)
	OnBuildWebLoginURL(p, a.buildWebLoginURL)
	OnExchangeLoginCode(p, a.exchangeLoginCode)
	OnApprovalTemplateGet(p, a.approvalTemplateGet)
	OnApprovalTemplateCreate(p, a.approvalTemplateCreate)
	OnApprovalTemplateUpdate(p, a.approvalTemplateUpdate)
	OnApprovalApply(p, a.approvalApply)
	OnApprovalGet(p, a.approvalGet)
	OnApprovalList(p, a.approvalList)
	OnOpenApprovalGet(p, a.openApprovalGet)
	OnAutoReplySet(p, a.autoReplySet)
	OnAutoReplyList(p, a.autoReplyList)
	OnAutoReplyDelete(p, a.autoReplyDelete)
	OnListReceived(p, a.listReceived)
}

// The manifest explicitly declares these objects opaque because message,
// menu, workbench and approval control schemas vary with upstream types.
func rawFeatureBody(body map[string]any) (json.RawMessage, error) {
	if body == nil {
		return nil, fmt.Errorf("请提供原生 JSON 对象")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("JSON 内容无法编码")
	}
	return b, nil
}
func featureResult(raw json.RawMessage) (map[string]any, error) {
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil || result == nil {
		return nil, fmt.Errorf("企业微信返回的结果不是有效对象")
	}
	return result, nil
}

func (a *featureApplication) sendMessage(ctx plugin.Ctx, in *SendMessageIn) (*SendMessageOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	b, err := rawFeatureBody(in.Body)
	if err != nil {
		return nil, err
	}
	d, err := a.client.SendAppMessage(ctx, c, in.Users, in.MessageType, b)
	if err != nil {
		return nil, err
	}
	return &SendMessageOut{MsgID: d.MsgID, ResponseCode: d.ResponseCode, InvalidUser: d.InvalidUser, UnlicensedUser: d.UnlicensedUser, AllAccepted: accepted(d.Delivery)}, nil
}

func (a *featureApplication) sendTemplateCard(ctx plugin.Ctx, in *SendTemplateCardIn) (*SendTemplateCardOut, error) {
	cardType, ok := in.Card["card_type"].(string)
	if !ok || strings.TrimSpace(cardType) == "" {
		return nil, fmt.Errorf("请提供包含 card_type 的企业微信 template_card 对象本身，无需包裹 template_card 字段")
	}
	out, err := a.sendMessage(ctx, &SendMessageIn{Users: in.Users, MessageType: "template_card", Body: map[string]any{"template_card": in.Card}})
	if err != nil {
		return nil, err
	}
	return &SendTemplateCardOut{MsgID: out.MsgID, ResponseCode: out.ResponseCode, InvalidUser: out.InvalidUser, UnlicensedUser: out.UnlicensedUser, AllAccepted: out.AllAccepted}, nil
}
func (a *featureApplication) uploadMedia(ctx plugin.Ctx, in *UploadMediaIn) (*UploadMediaOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	if in.File == nil || in.File.Name == "" {
		return nil, fmt.Errorf("请提供带文件名的素材文件")
	}
	if in.File.Size > 20*1024*1024 {
		return nil, fmt.Errorf("素材文件不能超过 20MB")
	}
	data, err := in.File.Blob(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取平台素材文件失败")
	}
	out, err := a.client.UploadMedia(ctx, c, in.MediaType, in.File.Name, data)
	if err != nil {
		return nil, err
	}
	return &UploadMediaOut{MediaID: out.MediaID, MediaType: out.Type, CreatedAt: int(out.CreatedAt)}, nil
}

func (a *featureApplication) getApplication(ctx plugin.Ctx, in *GetApplicationIn) (*GetApplicationOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := a.client.GetApplication(ctx, c)
	if err != nil {
		return nil, err
	}
	result, err := featureResult(raw)
	if err != nil {
		return nil, err
	}
	return &GetApplicationOut{Result: result}, nil
}

func (a *featureApplication) getMenu(ctx plugin.Ctx, in *GetMenuIn) (*GetMenuOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := a.client.GetMenu(ctx, c)
	if err != nil {
		return nil, err
	}
	result, err := featureResult(raw)
	if err != nil {
		return nil, err
	}
	return &GetMenuOut{Result: result}, nil
}

func (a *featureApplication) getWorkbenchTemplate(ctx plugin.Ctx, in *GetWorkbenchTemplateIn) (*GetWorkbenchTemplateOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := a.client.GetWorkbenchTemplate(ctx, c)
	if err != nil {
		return nil, err
	}
	result, err := featureResult(raw)
	if err != nil {
		return nil, err
	}
	return &GetWorkbenchTemplateOut{Result: result}, nil
}

func (a *featureApplication) getWorkbenchData(ctx plugin.Ctx, in *GetWorkbenchDataIn) (*GetWorkbenchDataOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := a.client.GetWorkbenchData(ctx, c, in.UserID)
	if err != nil {
		return nil, err
	}
	result, err := featureResult(raw)
	if err != nil {
		return nil, err
	}
	return &GetWorkbenchDataOut{Result: result}, nil
}

func (a *featureApplication) recallMessage(ctx plugin.Ctx, in *RecallMessageIn) (*RecallMessageOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.client.RecallMessage(ctx, c, in.MsgID); err != nil {
		return nil, err
	}
	return &RecallMessageOut{OK: true}, nil
}

func (a *featureApplication) setApplication(ctx plugin.Ctx, in *SetApplicationIn) (*SetApplicationOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	b, err := rawFeatureBody(in.Body)
	if err != nil {
		return nil, err
	}
	if err := a.client.SetApplication(ctx, c, b); err != nil {
		return nil, err
	}
	return &SetApplicationOut{OK: true}, nil
}

func (a *featureApplication) setMenu(ctx plugin.Ctx, in *SetMenuIn) (*SetMenuOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	b, err := rawFeatureBody(in.Body)
	if err != nil {
		return nil, err
	}
	if err := a.client.SetMenu(ctx, c, b); err != nil {
		return nil, err
	}
	return &SetMenuOut{OK: true}, nil
}

func (a *featureApplication) deleteMenu(ctx plugin.Ctx, in *DeleteMenuIn) (*DeleteMenuOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.client.DeleteMenu(ctx, c); err != nil {
		return nil, err
	}
	return &DeleteMenuOut{OK: true}, nil
}

func (a *featureApplication) setWorkbenchTemplate(ctx plugin.Ctx, in *SetWorkbenchTemplateIn) (*SetWorkbenchTemplateOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	b, err := rawFeatureBody(in.Body)
	if err != nil {
		return nil, err
	}
	if err := a.client.SetWorkbenchTemplate(ctx, c, b); err != nil {
		return nil, err
	}
	return &SetWorkbenchTemplateOut{OK: true}, nil
}

func (a *featureApplication) setWorkbenchData(ctx plugin.Ctx, in *SetWorkbenchDataIn) (*SetWorkbenchDataOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	b, err := rawFeatureBody(in.Body)
	if err != nil {
		return nil, err
	}
	if err := a.client.SetWorkbenchData(ctx, c, in.UserID, b); err != nil {
		return nil, err
	}
	return &SetWorkbenchDataOut{OK: true}, nil
}

func (a *featureApplication) batchSetWorkbenchData(ctx plugin.Ctx, in *BatchSetWorkbenchDataIn) (*BatchSetWorkbenchDataOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	b, err := rawFeatureBody(in.Body)
	if err != nil {
		return nil, err
	}
	if err := a.client.BatchSetWorkbenchData(ctx, c, in.Users, b); err != nil {
		return nil, err
	}
	return &BatchSetWorkbenchDataOut{OK: true}, nil
}

func (a *featureApplication) updateTemplateCard(ctx plugin.Ctx, in *UpdateTemplateCardIn) (*UpdateTemplateCardOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	b, err := rawFeatureBody(in.Body)
	if err != nil {
		return nil, err
	}
	d, err := a.client.UpdateTemplateCard(ctx, c, in.Users, in.ResponseCode, b)
	if err != nil {
		return nil, err
	}
	invalid := d.InvalidUsers
	if invalid == nil {
		invalid = []string{}
	}
	return &UpdateTemplateCardOut{InvalidUsers: invalid, AllUpdated: len(invalid) == 0}, nil
}
