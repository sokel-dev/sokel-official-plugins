package main

import (
	"context"
	"io"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type fakeCtx struct {
	context.Context
	cred map[string]string
}

func newFake(cred map[string]string) *fakeCtx {
	return &fakeCtx{Context: context.Background(), cred: cred}
}

func (f *fakeCtx) Credential() map[string]string { return f.cred }
func (f *fakeCtx) Upload(name, mime string, data []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f_test", Name: name, Mime: mime}, nil
}
func (f *fakeCtx) UploadReader(name, mime string, r io.Reader) (*plugin.File, error) {
	return &plugin.File{ID: "f_test", Name: name, Mime: mime}, nil
}
func (f *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return []byte("bytes"), nil }
