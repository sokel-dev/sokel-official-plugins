// synology-upload — a first-party Sokel plugin: upload files to a Synology NAS through DSM's FileStation API.
//
// Kept apart from the synology plugin on purpose (see the top of schema/schema.go): that one works on a directory
// bind-mounted into its container, and files a container writes there never reach Synology Drive. This one logs in to
// DSM with an account, so the NAS treats the upload like one from File Station, and it runs anywhere the NAS answers.
//
// Run: SOKEL_ENDPOINT=https://<platform> SOKEL_TOKEN=skp_xxx ./synology-upload
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/synology-upload/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "synology-upload",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnUploadFile(p, uploadFile)
	OnHealthCheck(p, healthCheck) // platform convention: the credential page's "Test" and workflows' "Check credential"

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// clientOf builds the DSM client and checks the credential has what a login needs.
func clientOf(cred Cred) (*dsmClient, error) {
	if strings.TrimSpace(cred.Username) == "" || cred.Password == "" {
		return nil, fmt.Errorf("凭证缺账号或密码")
	}
	return newDSMClient(cred.URL, strings.EqualFold(strings.TrimSpace(cred.InsecureTLS), "true"))
}

func uploadFile(ctx plugin.Ctx, in *UploadFileIn) (*UploadFileOut, error) {
	dir := strings.TrimSpace(in.Dir)
	if !strings.HasPrefix(dir, "/") || dir == "/" {
		return nil, fmt.Errorf("目标文件夹 %q 不对：要以共享文件夹开头，如 /research/入库", in.Dir)
	}
	dir = strings.TrimRight(dir, "/")
	if len(in.Files) == 0 {
		return nil, fmt.Errorf("没有要上传的文件")
	}
	onConflict := in.OnConflict
	if onConflict == "" {
		onConflict = "error"
	}
	cred := sokel.CredentialAs[Cred](ctx)
	c, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	sid, err := c.login(ctx, cred.Username, cred.Password)
	if err != nil {
		return nil, err
	}
	defer c.logout(sid)

	out := &UploadFileOut{Files: make([]schema.Uploaded, 0, len(in.Files))}
	for i, f := range in.Files {
		if f == nil {
			return nil, fmt.Errorf("第 %d 个文件为空", i+1)
		}
		name := path.Base(strings.ReplaceAll(strings.TrimSpace(f.Name), "\\", "/"))
		if name == "" || name == "." || name == "/" {
			return nil, fmt.Errorf("第 %d 个文件没有文件名", i+1)
		}
		data, err := ctx.Fetch(f)
		if err != nil {
			return nil, fmt.Errorf("读取平台文件 %s 失败: %w", name, err)
		}
		// Files already sent stay on the NAS when a later one fails; the error names the file it stopped at.
		skipped, err := c.upload(ctx, sid, dir, name, data, onConflict, in.CreateParents)
		if err != nil {
			if out.Uploaded > 0 {
				return nil, fmt.Errorf("%w（此前 %d 个已传完）", err, out.Uploaded)
			}
			return nil, err
		}
		out.Files = append(out.Files, schema.Uploaded{Name: name, Path: dir + "/" + name, Size: int64(len(data)), Skipped: skipped})
		if skipped {
			out.Skipped++
		} else {
			out.Uploaded++
		}
	}
	return out, nil
}

// healthCheck logs in once. Unavailable is ok=false + message, not an error: an error would only show a red cross,
// while the message tells a wrong address from a certificate, a password or a two-step-verification problem.
func healthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	cred := sokel.CredentialAs[Cred](ctx)
	c, err := clientOf(cred)
	if err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	sid, err := c.login(ctx, cred.Username, cred.Password)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "certificate") || strings.Contains(msg, "x509") {
			msg += "（DSM 用的是自签证书时，可在凭证里把「跳过证书校验」填 true）"
		}
		return &HealthCheckOut{Message: msg}, nil
	}
	c.logout(sid)
	return &HealthCheckOut{OK: true, Message: "已登录 DSM（" + c.base + "）"}, nil
}
