package main

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func (a *featureApplication) jsSDKConfig(ctx plugin.Ctx, in *JsSdkConfigIn) (*JsSdkConfigOut, error) {
	cred, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	config, err := a.identity.JSConfig(ctx, cred, in.PageURL)
	if err != nil {
		return nil, err
	}
	if int64(int(config.AgentID)) != config.AgentID || int64(int(config.Timestamp)) != config.Timestamp {
		return nil, errors.New("当前运行平台不支持此 AgentID 或时间戳的整数范围")
	}
	return &JsSdkConfigOut{CorpID: config.CorpID, AgentID: int(config.AgentID), NonceStr: config.NonceStr, Timestamp: int(config.Timestamp), Signature: config.Signature, AgentSignature: config.AgentSignature, SignedURL: config.SignedURL}, nil
}

func (a *featureApplication) buildWebLoginURL(ctx plugin.Ctx, in *BuildWebLoginURLIn) (*BuildWebLoginURLOut, error) {
	cred, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	link, err := BuildWebLoginURL(cred, in.RedirectURL, in.State, in.Language)
	if err != nil {
		return nil, err
	}
	return &BuildWebLoginURLOut{URL: link}, nil
}

func (a *featureApplication) exchangeLoginCode(ctx plugin.Ctx, in *ExchangeLoginCodeIn) (*ExchangeLoginCodeOut, error) {
	cred, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	userID, err := a.client.UserID(ctx, cred, in.Code)
	if err != nil {
		return nil, err
	}
	return &ExchangeLoginCodeOut{UserID: userID}, nil
}

func (a *featureApplication) approvalTemplateGet(ctx plugin.Ctx, in *ApprovalTemplateGetIn) (*ApprovalTemplateGetOut, error) {
	cred, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := a.client.ApprovalTemplate(ctx, cred, in.TemplateID)
	if err != nil {
		return nil, err
	}
	result, err := decodeIdentityApprovalResult(raw)
	if err != nil {
		return nil, err
	}
	return &ApprovalTemplateGetOut{Result: result}, nil
}

func (a *featureApplication) approvalTemplateCreate(ctx plugin.Ctx, in *ApprovalTemplateCreateIn) (*ApprovalTemplateCreateOut, error) {
	cred, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	body, err := marshalIdentityApprovalBody(in.Body)
	if err != nil {
		return nil, err
	}
	id, err := a.client.CreateApprovalTemplate(ctx, cred, body)
	if err != nil {
		return nil, err
	}
	return &ApprovalTemplateCreateOut{TemplateID: id}, nil
}

func (a *featureApplication) approvalTemplateUpdate(ctx plugin.Ctx, in *ApprovalTemplateUpdateIn) (*ApprovalTemplateUpdateOut, error) {
	cred, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	body, err := marshalIdentityApprovalBody(in.Body)
	if err != nil {
		return nil, err
	}
	if err := a.client.UpdateApprovalTemplate(ctx, cred, body); err != nil {
		return nil, err
	}
	return &ApprovalTemplateUpdateOut{OK: true}, nil
}

func (a *featureApplication) approvalApply(ctx plugin.Ctx, in *ApprovalApplyIn) (*ApprovalApplyOut, error) {
	cred, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	body, err := marshalIdentityApprovalBody(in.Body)
	if err != nil {
		return nil, err
	}
	id, err := a.client.ApplyApproval(ctx, cred, body)
	if err != nil {
		return nil, err
	}
	return &ApprovalApplyOut{SpNo: id}, nil
}

func (a *featureApplication) approvalGet(ctx plugin.Ctx, in *ApprovalGetIn) (*ApprovalGetOut, error) {
	cred, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := a.client.ApprovalDetail(ctx, cred, in.SpNo)
	if err != nil {
		return nil, err
	}
	result, err := decodeIdentityApprovalResult(raw)
	if err != nil {
		return nil, err
	}
	return &ApprovalGetOut{Result: result}, nil
}

func (a *featureApplication) approvalList(ctx plugin.Ctx, in *ApprovalListIn) (*ApprovalListOut, error) {
	cred, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	body, err := marshalIdentityApprovalBody(in.Body)
	if err != nil {
		return nil, err
	}
	result, err := a.client.ApprovalList(ctx, cred, body)
	if err != nil {
		return nil, err
	}
	return &ApprovalListOut{SpNoList: result.SPNoList, NextCursor: result.NextCursor}, nil
}

func (a *featureApplication) openApprovalGet(ctx plugin.Ctx, in *OpenApprovalGetIn) (*OpenApprovalGetOut, error) {
	cred, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := a.client.OpenApprovalData(ctx, cred, in.ThirdNo)
	if err != nil {
		return nil, err
	}
	result, err := decodeIdentityApprovalResult(raw)
	if err != nil {
		return nil, err
	}
	return &OpenApprovalGetOut{Result: result}, nil
}

func marshalIdentityApprovalBody(value map[string]any) (json.RawMessage, error) {
	if value == nil {
		return nil, errors.New("请提供审批 body JSON 对象")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("审批 body 不是可序列化的 JSON 对象")
	}
	if len(b) > 1024*1024 {
		return nil, errors.New("审批 body 超过 1 MiB 大小限制")
	}
	return json.RawMessage(b), nil
}

func decodeIdentityApprovalResult(raw json.RawMessage) (map[string]any, error) {
	var out map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	// Preserve large upstream numeric IDs rather than silently rounding floats.
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil || out == nil {
		return nil, errors.New("企业微信审批结果不是有效的 JSON 对象")
	}
	return out, nil
}
