package store_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestIssueTagsCRUDAttachHydrateAndFilter(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)

	customer, err := env.store.CreateIssueTag(env.ctx, store.CreateIssueTagParams{
		ProjectID: env.projectID,
		Name:      " #Customer   Beta ",
		Color:     model.TagColorGreen,
	})
	if err != nil {
		t.Fatalf("CreateIssueTag customer: %v", err)
	}
	if customer.Ref != "tag-1" || customer.Name != "Customer Beta" || customer.DisplayName != "#Customer Beta" || customer.Color != model.TagColorGreen {
		t.Fatalf("customer tag mismatch: %+v", customer)
	}

	launch, err := env.store.CreateIssueTag(env.ctx, store.CreateIssueTagParams{
		ProjectID: env.projectID,
		Name:      "#Q3 Launch",
	})
	if err != nil {
		t.Fatalf("CreateIssueTag launch: %v", err)
	}
	if launch.Ref != "tag-2" || launch.Color != model.TagColorBlue {
		t.Fatalf("launch tag mismatch: %+v", launch)
	}
	gotByID, err := env.store.GetIssueTag(env.ctx, customer.ID)
	if err != nil {
		t.Fatalf("GetIssueTag: %v", err)
	}
	if gotByID.ID != customer.ID {
		t.Fatalf("GetIssueTag ID = %s, want %s", gotByID.ID, customer.ID)
	}
	gotByNumber, err := env.store.GetIssueTagByProjectNumber(env.ctx, env.projectID, customer.Number)
	if err != nil {
		t.Fatalf("GetIssueTagByProjectNumber: %v", err)
	}
	if gotByNumber.ID != customer.ID {
		t.Fatalf("GetIssueTagByProjectNumber ID = %s, want %s", gotByNumber.ID, customer.ID)
	}
	gotByName, err := env.store.GetIssueTagByProjectName(env.ctx, env.projectID, "#Customer Beta")
	if err != nil {
		t.Fatalf("GetIssueTagByProjectName: %v", err)
	}
	if gotByName.ID != customer.ID {
		t.Fatalf("GetIssueTagByProjectName ID = %s, want %s", gotByName.ID, customer.ID)
	}
	firstPage, more, err := env.store.ListIssueTags(env.ctx, store.ListIssueTagsParams{ProjectID: env.projectID, Limit: 1})
	if err != nil {
		t.Fatalf("ListIssueTags page 1: %v", err)
	}
	if !more || len(firstPage) != 1 || firstPage[0].ID != customer.ID {
		t.Fatalf("ListIssueTags page 1 = %+v more=%v", firstPage, more)
	}
	secondPage, more, err := env.store.ListIssueTags(env.ctx, store.ListIssueTagsParams{
		ProjectID: env.projectID,
		Cursor:    &store.IssueTagsCursor{Number: firstPage[0].Number},
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListIssueTags page 2: %v", err)
	}
	if more || len(secondPage) != 1 || secondPage[0].ID != launch.ID {
		t.Fatalf("ListIssueTags page 2 = %+v more=%v", secondPage, more)
	}

	if _, err := env.store.CreateIssueTag(env.ctx, store.CreateIssueTagParams{ProjectID: env.projectID, Name: "#Customer   Beta"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate CreateIssueTag err = %v, want ErrConflict", err)
	}
	if _, err := env.store.CreateIssueTag(env.ctx, store.CreateIssueTagParams{ProjectID: env.projectID, Name: "Invalid Color", Color: model.IssueTagColor("mauve")}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("invalid color err = %v, want ErrConflict", err)
	}

	renamed := "#Q3  Launch  FY26"
	pink := model.TagColorPink
	launch, err = env.store.UpdateIssueTag(env.ctx, store.UpdateIssueTagParams{
		ID:    launch.ID,
		Name:  &renamed,
		Color: &pink,
	})
	if err != nil {
		t.Fatalf("UpdateIssueTag: %v", err)
	}
	if launch.Name != "Q3 Launch FY26" || launch.DisplayName != "#Q3 Launch FY26" || launch.Color != model.TagColorPink {
		t.Fatalf("updated launch mismatch: %+v", launch)
	}
	dupe := "Customer Beta"
	if _, err := env.store.UpdateIssueTag(env.ctx, store.UpdateIssueTagParams{ID: launch.ID, Name: &dupe}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate UpdateIssueTag err = %v, want ErrConflict", err)
	}

	parent := mustCreateIssue(t, env, "Parent")
	child, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{ParentIssueID: parent.ID, Title: "Child"})
	if err != nil {
		t.Fatalf("CreateSubIssue: %v", err)
	}
	plain := mustCreateIssue(t, env, "Plain")

	parentLink, err := env.store.CreateIssueTagLink(env.ctx, store.CreateIssueTagLinkParams{IssueID: parent.ID, TagID: customer.ID})
	if err != nil {
		t.Fatalf("attach customer to parent: %v", err)
	}
	if _, err := env.store.CreateIssueTagLink(env.ctx, store.CreateIssueTagLinkParams{IssueID: child.ID, TagID: launch.ID}); err != nil {
		t.Fatalf("attach launch to child: %v", err)
	}
	tagProjectID, err := env.store.ProjectIDForIssueTag(env.ctx, customer.ID)
	if err != nil {
		t.Fatalf("ProjectIDForIssueTag: %v", err)
	}
	if tagProjectID != env.projectID {
		t.Fatalf("ProjectIDForIssueTag = %s, want %s", tagProjectID, env.projectID)
	}
	linkProjectID, err := env.store.ProjectIDForIssueTagLink(env.ctx, parentLink.ID)
	if err != nil {
		t.Fatalf("ProjectIDForIssueTagLink: %v", err)
	}
	if linkProjectID != env.projectID {
		t.Fatalf("ProjectIDForIssueTagLink = %s, want %s", linkProjectID, env.projectID)
	}
	if _, err := env.store.CreateIssueTagLink(env.ctx, store.CreateIssueTagLinkParams{IssueID: parent.ID, TagID: customer.ID}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate attach err = %v, want ErrConflict", err)
	}

	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	otherProject, err := env.store.CreateProjectForUser(env.ctx, project.OwnerID, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser other: %v", err)
	}
	otherIssue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: otherProject.ID, Title: "Other"})
	if err != nil {
		t.Fatalf("CreateIssue other: %v", err)
	}
	if _, err := env.store.CreateIssueTagLink(env.ctx, store.CreateIssueTagLinkParams{IssueID: otherIssue.ID, TagID: customer.ID}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-project attach err = %v, want ErrConflict", err)
	}

	gotParent, err := env.store.GetIssue(env.ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetIssue parent: %v", err)
	}
	if gotTagNames(gotParent.Tags) != "#Customer Beta" {
		t.Fatalf("parent tags = %+v", gotParent.Tags)
	}
	children, _, err := env.store.ListSubIssuesForIssue(env.ctx, store.ListSubIssuesForIssueParams{ParentIssueID: parent.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListSubIssuesForIssue: %v", err)
	}
	if len(children) != 1 || gotTagNames(children[0].Tags) != "#Q3 Launch FY26" {
		t.Fatalf("children = %+v", children)
	}

	customerIssues, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID:        env.projectID,
		IncludeSubIssues: true,
		TagNames:         []string{"#Customer Beta"},
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListIssues customer tag: %v", err)
	}
	if !reflect.DeepEqual(gotIssueIDs(customerIssues), []uuid.UUID{parent.ID}) {
		t.Fatalf("customer filtered issues = %+v", customerIssues)
	}

	anyTagged, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID:        env.projectID,
		IncludeSubIssues: true,
		TagNames:         []string{"Customer Beta", "#Q3 Launch FY26"},
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListIssues any tag: %v", err)
	}
	if !reflect.DeepEqual(gotIssueIDs(anyTagged), []uuid.UUID{parent.ID, child.ID}) {
		t.Fatalf("any tag IDs = %v, want parent+child", gotIssueIDs(anyTagged))
	}

	untagged, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID:        env.projectID,
		IncludeSubIssues: true,
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListIssues unfiltered: %v", err)
	}
	if !reflect.DeepEqual(gotIssueIDs(untagged), []uuid.UUID{parent.ID, child.ID, plain.ID}) {
		t.Fatalf("unfiltered IDs = %v", gotIssueIDs(untagged))
	}

	if err := env.store.DeleteIssueTagLink(env.ctx, parent.ID, customer.ID); err != nil {
		t.Fatalf("DeleteIssueTagLink: %v", err)
	}
	tags, _, err := env.store.ListTagsForIssue(env.ctx, store.ListTagsForIssueParams{IssueID: parent.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListTagsForIssue after detach: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("tags after detach = %+v, want empty", tags)
	}

	if err := env.store.DeleteIssueTag(env.ctx, launch.ID); err != nil {
		t.Fatalf("DeleteIssueTag launch: %v", err)
	}
	tags, _, err = env.store.ListTagsForIssue(env.ctx, store.ListTagsForIssueParams{IssueID: child.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListTagsForIssue after tag delete: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("tags after tag delete = %+v, want cascade empty", tags)
	}
}

func gotIssueIDs(issues []model.Issue) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(issues))
	for _, issue := range issues {
		out = append(out, issue.ID)
	}
	return out
}

func gotTagNames(tags []model.IssueTag) string {
	if len(tags) == 0 {
		return ""
	}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		out = append(out, tag.DisplayName)
	}
	return strings.Join(out, ",")
}
