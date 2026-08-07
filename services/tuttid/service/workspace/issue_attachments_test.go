package workspace

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	workspaceissues "github.com/tutti-os/tutti/packages/workspace/issues"
	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
)

type memoryIssueAttachmentFiles struct {
	files map[string][]byte
}

func newMemoryIssueAttachmentFiles() *memoryIssueAttachmentFiles {
	return &memoryIssueAttachmentFiles{files: make(map[string][]byte)}
}

func (f *memoryIssueAttachmentFiles) WriteExclusive(attachmentID string, extension string, data []byte) (string, error) {
	path := "/managed/issues/" + attachmentID + extension
	if _, exists := f.files[path]; exists {
		return "", fs.ErrExist
	}
	f.files[path] = append([]byte(nil), data...)
	return path, nil
}

func (f *memoryIssueAttachmentFiles) Remove(path string) error {
	delete(f.files, path)
	return nil
}

func (*memoryIssueAttachmentFiles) IsManagedPath(path string) bool {
	return strings.HasPrefix(path, "/managed/issues/")
}

func TestIssueAttachmentValidationPersistsPNG(t *testing.T) {
	files := newMemoryIssueAttachmentFiles()
	service := IssueManagerService{AttachmentFiles: files}
	data := []byte("\x89PNG\r\n\x1a\ncontent")

	refs, err := service.persistIssueAttachments([]CreateIssueManagerImageAttachmentInput{{
		AttachmentID: "fbeec26b-1dde-4509-9368-f40c78e24a38",
		DisplayName:  "capture.png",
		MimeType:     "image/png",
		Data:         data,
	}})
	if err != nil {
		t.Fatalf("persistIssueAttachments() error = %v", err)
	}
	if len(refs) != 1 || refs[0].RefType != "image/png" || refs[0].DisplayName != "capture.png" {
		t.Fatalf("persistIssueAttachments() refs = %#v", refs)
	}
	if got := files.files[refs[0].Path]; string(got) != string(data) {
		t.Fatalf("persisted data = %q, want %q", got, data)
	}
}

func TestIssueAttachmentValidationRejectsMismatchedImageData(t *testing.T) {
	service := IssueManagerService{AttachmentFiles: newMemoryIssueAttachmentFiles()}
	_, err := service.persistIssueAttachments([]CreateIssueManagerImageAttachmentInput{{
		MimeType: "image/png",
		Data:     []byte("not a png"),
	}})
	if !errors.Is(err, workspaceissues.ErrInvalidArgument) {
		t.Fatalf("persistIssueAttachments() error = %v, want ErrInvalidArgument", err)
	}
}

func TestIssueAttachmentValidationDoesNotOverwriteExistingAttachment(t *testing.T) {
	files := newMemoryIssueAttachmentFiles()
	service := IssueManagerService{AttachmentFiles: files}
	const attachmentID = "fbeec26b-1dde-4509-9368-f40c78e24a38"
	first := []byte("\x89PNG\r\n\x1a\nfirst")
	input := CreateIssueManagerImageAttachmentInput{
		AttachmentID: attachmentID,
		MimeType:     "image/png",
		Data:         first,
	}
	refs, err := service.persistIssueAttachments([]CreateIssueManagerImageAttachmentInput{input})
	if err != nil {
		t.Fatalf("first persistIssueAttachments() error = %v", err)
	}
	input.Data = []byte("\x89PNG\r\n\x1a\nsecond")
	if _, err := service.persistIssueAttachments([]CreateIssueManagerImageAttachmentInput{input}); !errors.Is(err, workspaceissues.ErrInvalidArgument) {
		t.Fatalf("second persistIssueAttachments() error = %v, want ErrInvalidArgument", err)
	}
	if got := files.files[refs[0].Path]; string(got) != string(first) {
		t.Fatalf("existing attachment = %q, want %q", got, first)
	}
}

func TestIssueManagerCreateIssuePersistsProjectsAndDeletesAttachment(t *testing.T) {
	ctx := context.Background()
	workspaceStore := openIssueServiceStore(t)
	const workspaceID = "workspace-screenshot-attachment"
	if err := workspaceStore.Create(ctx, workspacebiz.Summary{ID: workspaceID, Name: "Screenshot attachment"}); err != nil {
		t.Fatal(err)
	}
	files := newMemoryIssueAttachmentFiles()
	service := IssueManagerService{AttachmentFiles: files, Store: workspaceStore}
	issue, err := service.CreateIssue(ctx, workspaceID, CreateIssueManagerIssueInput{
		TopicID: workspaceissues.DefaultTopicID,
		Title:   "Inspect screenshot",
		Attachments: []CreateIssueManagerImageAttachmentInput{{
			DisplayName: "capture.png",
			MimeType:    "image/png",
			Data:        []byte("\x89PNG\r\n\x1a\ncontent"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateIssue() error = %v", err)
	}
	detail, err := service.GetIssueDetail(ctx, workspaceID, issue.IssueID)
	if err != nil {
		t.Fatalf("GetIssueDetail() error = %v", err)
	}
	if len(detail.ContextRefs) != 1 {
		t.Fatalf("ContextRefs = %#v, want one attachment", detail.ContextRefs)
	}
	attachmentPath := detail.ContextRefs[0].Path
	launchAttachments, err := service.issueRunImageAttachments(ctx, issue, workspaceissues.Task{})
	if err != nil {
		t.Fatalf("issueRunImageAttachments() error = %v", err)
	}
	if len(launchAttachments) != 1 || launchAttachments[0].Path != attachmentPath || launchAttachments[0].MimeType != "image/png" {
		t.Fatalf("issueRunImageAttachments() = %#v", launchAttachments)
	}
	removed, err := service.DeleteIssue(ctx, workspaceID, issue.IssueID)
	if err != nil || !removed {
		t.Fatalf("DeleteIssue() removed = %v, error = %v", removed, err)
	}
	if _, exists := files.files[attachmentPath]; exists {
		t.Fatal("attachment still exists after DeleteIssue()")
	}
}
