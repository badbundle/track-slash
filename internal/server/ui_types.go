package server

import (
	"html/template"
	"time"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
)

type uiLoginData struct {
	Error string
	Next  string
}

type uiSignupData struct {
	Error string
	Next  string
}

type uiShellData struct {
	User              model.User
	Projects          []model.Project
	SidebarFavorites  uiSidebarFavoritesData
	CurrentProjectID  uuid.UUID
	CurrentView       string
	WorkPanel         *uiWorkPanelData
	ProjectsPanel     *uiProjectsPanelData
	NewProjectPanel   *uiNewProjectPanelData
	NewIssuePanel     *uiNewIssuePanelData
	ProjectPanel      *uiProjectPanelData
	DeletedPanel      *uiDeletedIssuesPanelData
	DeletedIssuePanel *uiDeletedIssuePanelData
	IssuePanel        *uiIssuePanelData
	ContextManager    *uiContextManagerData
	TagManager        *uiTagManagerData
	TokenPanel        *uiTokenPanelData
	SettingsPanel     *uiSettingsPanelData
}

type uiSidebarFavoritesData struct {
	Projects         []model.Project
	CurrentProjectID uuid.UUID
	OOB              bool
}

type uiUserAvatarData struct {
	ID           uuid.UUID
	Label        string
	Initials     string
	ThumbnailURL string
	Class        string
}

type uiProjectIconData struct {
	Label        string
	Initial      string
	ThumbnailURL string
	Class        string
}

type uiProjectFavoriteData struct {
	Project  model.Project
	View     string
	Favorite bool
	Sidebar  uiSidebarFavoritesData
}

type uiBreadcrumbData struct {
	Items []uiBreadcrumbItem
}

type uiBreadcrumbItem struct {
	Label    string
	Href     string
	HXGet    string
	IssueKey bool
	Current  bool
}

type uiIssueItem struct {
	Issue    model.Issue
	Project  model.Project
	Sprint   *model.Sprint
	Assignee *model.ProjectAssignee
}

type uiIssueColumn struct {
	Status model.Status
	Label  string
	Issues []uiIssueItem
}

type uiSprintDescriptionData struct {
	Project             model.Project
	Sprint              model.Sprint
	AttachmentCount     int
	DescriptionExpanded bool
	DescriptionHTML     template.HTML
	Attachments         []model.SprintAttachment
	AttachmentsHasMore  bool
}

type uiPlannedSprint struct {
	Project             model.Project
	Sprint              model.Sprint
	Issues              []model.Issue
	HasMore             bool
	AttachmentCount     int
	DescriptionExpanded bool
	DescriptionHTML     template.HTML
	Attachments         []model.SprintAttachment
	AttachmentsHasMore  bool
}

type uiSprintFormData struct {
	NameInput      string
	GoalInput      string
	StartDateInput string
	EndDateInput   string
	Error          string
}

type uiDescriptionAttachment struct {
	Object         model.StorageObject
	ContentHref    string
	InlineHref     string
	DeleteHref     string
	DeleteJSONHref string
	Markdown       string
	InlineImage    bool
}

type uiAttachmentListData struct {
	ID        string
	Items     []uiDescriptionAttachment
	HasMore   bool
	Editing   bool
	UploadURL string
}

type uiDescriptionEditorData struct {
	Name        string
	Source      string
	Rows        int
	Autofocus   bool
	UploadURL   string
	ListTarget  string
	Placeholder string
}

type uiDescriptionBodyData struct {
	Source     string
	HTML       template.HTML
	EmptyLabel string
}

type uiSprintIssueFormData struct {
	IssueInput string
	Error      string
}

type uiAssigneeFilterItem struct {
	Assignee model.ProjectAssignee
	Selected bool
	Href     string
	HXGet    string
	HXPush   string
}

type uiTagFilterItem struct {
	Tag      model.IssueTag
	Label    string
	Selected bool
	Href     string
	HXGet    string
	HXPush   string
}

type uiProjectStatusFilterItem struct {
	Label  string
	Href   string
	HXGet  string
	HXPush string
	Active bool
}

type uiProjectPriorityFilterItem struct {
	Priority model.IssuePriority
	Label    string
	Href     string
	HXGet    string
	HXPush   string
	Active   bool
}

type uiProjectSortOptionItem struct {
	Label  string
	Icon   string
	Href   string
	HXGet  string
	HXPush string
	Active bool
}

type uiIssueControlsData struct {
	StatusFilters        []uiProjectStatusFilterItem
	PriorityFilters      []uiProjectPriorityFilterItem
	TagFilters           []uiTagFilterItem
	ActiveFilterCount    int
	SortOptions          []uiProjectSortOptionItem
	SortLabel            string
	DirectionOptions     []uiProjectSortOptionItem
	DirectionLabel       string
	DirectionIcon        string
	AssigneeFilters      []uiAssigneeFilterItem
	AssigneeFilterActive bool
	ClearAssigneeHref    string
	ClearAssigneeHXGet   string
	ClearAssigneeHXPush  string
}

type uiProjectAllIssuePageData struct {
	Issues    []model.Issue
	NextHXGet string
}

type uiIssueCommentItem struct {
	Comment                             model.Comment
	AuthorID                            uuid.UUID
	AuthorUsername                      string
	AuthorName                          string
	AuthorEmail                         string
	AuthorProfileImageThumbnailObjectID *uuid.UUID
	CanEdit                             bool
}

type uiIssueLinkItem struct {
	Link        model.IssueLink
	LinkedIssue model.Issue
	HasIssue    bool
}

type uiProjectContextItem struct {
	Context             model.ProjectContextSummary
	LinkedIssues        []model.Issue
	LinkedIssuesHasMore bool
	LinkIssueInput      string
	LinkIssueError      string
}

type uiProjectContextOption struct {
	Value string
	Label string
}

type uiContextManagerItem struct {
	ID                  uuid.UUID
	Ref                 string
	Number              int
	Scope               model.ProjectContextScope
	Position            *int64
	Title               string
	ContentType         string
	SourceFilename      *string
	LinkedIssueCount    int
	LinkedIssues        []model.Issue
	LinkedIssuesHasMore bool
	UpdatedAt           time.Time
}

type uiContextManagerData struct {
	Mode               string
	Action             string
	Project            model.Project
	Issue              model.Issue
	HasIssue           bool
	CanWrite           bool
	BackHref           string
	BackHXGet          string
	BackLabel          string
	Items              []uiContextManagerItem
	HasMore            bool
	ContextOptions     []uiProjectContextOption
	ActiveContextID    uuid.UUID
	HasActiveContext   bool
	ActiveContext      model.ProjectContext
	ActiveHTML         template.HTML
	Attachments        []model.ContextAttachment
	AttachmentsHasMore bool
	ContextInput       string
	ContextTitle       string
	ContextBody        string
	ContextError       string
	ContextCreateError string
	ContextUploadError string
	ContextEditTitle   string
	ContextEditBody    string
	ContextEditError   string
	LinkIssueInput     string
	LinkIssueError     string
}

type uiIssueSprintOption struct {
	Value string
	Label string
}

type uiAutocompleteOption struct {
	Value       string
	Label       string
	Badge       string
	SearchText  string
	TargetValue string
}

type uiOptionDropdownData struct {
	Action       string
	HXTarget     string
	HXPushURL    string
	CancelHXGet  string
	ToggleLabel  string
	ListLabel    string
	Name         string
	CurrentValue string
	CurrentLabel string
	CurrentClass string
	Error        string
	Options      []uiOptionDropdownOption
}

type uiOptionDropdownOption struct {
	Value string
	Label string
	Class string
}

type uiAutocompleteEditData struct {
	ID                string
	Label             string
	Action            string
	PanelPath         string
	IssueHref         string
	Name              string
	Value             string
	HiddenName        string
	HiddenValue       string
	TargetName        string
	Placeholder       string
	SaveLabel         string
	CancelLabel       string
	Error             string
	Autofocus         bool
	Collapsible       bool
	OptionsOpen       bool
	InputHXGet        string
	InputHXTrigger    string
	InputHXTarget     string
	InputHXSwap       string
	InputHXInclude    string
	InputHXPushURL    string
	SearchClearTarget string
	OptionsID         string
	EmptyLabel        string
	Options           []uiAutocompleteOption
}

type uiModalData struct {
	ID              string
	Title           string
	Description     string
	WidthClass      string
	CancelLabel     string
	CancelHXGet     string
	CancelHXPushURL string
	Badges          []uiModalBadge
}

type uiModalBadge struct {
	Label string
	Class string
}

type uiIssueDeleteNotice struct {
	Issue    model.Issue
	CanWrite bool
}

type uiTabBarData struct {
	Label string
	Items []uiTabItem
}

type uiTabItem struct {
	Label          string
	Icon           string
	Href           string
	HXGet          string
	HXTarget       string
	HXPushURL      string
	Active         bool
	MobileOverflow bool
}

type uiWorkPanelData struct {
	View           string
	Title          string
	Subtitle       string
	IssueListLabel string
	Issues         []uiIssueItem
	Columns        []uiIssueColumn
	HasMore        bool
	ProjectCount   int
	WorkTabs       uiTabBarData
	IssueControls  uiIssueControlsData
}

type uiProjectPanelData struct {
	Project                         model.Project
	View                            string
	CanWrite                        bool
	CanManageMembers                bool
	MembersPage                     bool
	Members                         []model.ProjectMember
	MemberCandidates                []model.ProjectMemberCandidate
	MemberInput                     string
	MemberRoleInput                 model.ProjectMemberRole
	MemberError                     string
	Favorite                        bool
	ProjectTabs                     uiTabBarData
	EditProjectName                 bool
	ProjectNameInput                string
	ProjectNameError                string
	EditProjectDescription          bool
	ProjectDescriptionInput         string
	ProjectDescriptionError         string
	ProjectDescriptionHTML          template.HTML
	ProjectAttachments              []model.ProjectAttachment
	ProjectAttachmentsHasMore       bool
	AssigneeFilters                 []uiAssigneeFilterItem
	AssigneeFilterActive            bool
	ClearAssigneeHref               string
	ClearAssigneeHXGet              string
	ClearAssigneeHXPush             string
	ActiveSprint                    *model.Sprint
	ActiveSprintDescription         uiSprintDescriptionData
	ActiveSprintAction              string
	ActiveSprintForm                uiSprintFormData
	ActiveSprintIssueForm           uiSprintIssueFormData
	SprintColumns                   []uiIssueColumn
	SprintControls                  uiIssueControlsData
	PlannedSprints                  []uiPlannedSprint
	NewSprint                       bool
	NewSprintForm                   uiSprintFormData
	PlannedSprintActionID           uuid.UUID
	PlannedSprintAction             string
	PlannedSprintForm               uiSprintFormData
	PlannedSprintIssueForm          uiSprintIssueFormData
	PlannedSprintAttachments        []model.SprintAttachment
	PlannedSprintAttachmentsHasMore bool
	AllIssues                       []model.Issue
	AllIssuePage                    uiProjectAllIssuePageData
	AllControls                     uiIssueControlsData
	ChangelogPage                   uiProjectChangelogPageData
	ProjectStats                    model.ProjectStats
	Tags                            []model.IssueTag
	ContextItems                    []uiProjectContextItem
	ContextHasMore                  bool
	ContextManager                  *uiContextManagerData
	DeleteNotice                    *uiIssueDeleteNotice
	SprintIssuesHasMore             bool
	PlannedHasMore                  bool
}

const uiIssueListDefaultSort = store.ListIssuesSortUpdated
const uiProjectAllDefaultSort = uiIssueListDefaultSort

type uiIssueListQuery struct {
	Statuses    []model.Status
	Priorities  []model.IssuePriority
	TagNames    []string
	Sort        store.ListIssuesSort
	Direction   store.ListIssuesSortDirection
	AssigneeIDs []uuid.UUID
	Cursor      string
}

type uiProjectAllQuery = uiIssueListQuery

type uiProjectChangelogPageData struct {
	Project    model.Project
	Entries    []model.ProjectChangelogEntry
	HasMore    bool
	NextCursor string
}

type uiDeletedIssuesPanelData struct {
	Project  model.Project
	Issues   []model.Issue
	HasMore  bool
	CanWrite bool
}

type uiDeletedIssuePanelData struct {
	Issue     model.Issue
	Project   model.Project
	CanWrite  bool
	BackHref  string
	BackHXGet string
}

type uiIssuePanelData struct {
	Issue              model.Issue
	Project            model.Project
	CanWrite           bool
	ParentIssue        *model.Issue
	Sprint             *model.Sprint
	Assignee           *model.User
	Reporter           *model.User
	EditTitle          bool
	EditDescription    bool
	EditStatus         bool
	PendingCloseReason bool
	EditCloseReason    bool
	EditPriority       bool
	EditDueDate        bool
	EditAssignee       bool
	EditReporter       bool
	EditSprint         bool
	CanEditSprint      bool
	DescriptionHTML    template.HTML
	Attachments        []model.IssueAttachment
	AttachmentsHasMore bool
	TitleInput         string
	AssigneeInput      string
	ReporterInput      string
	SprintInput        string
	DueDateInput       string
	CloseReasonInput   string
	AssigneeError      string
	ReporterError      string
	SprintError        string
	TitleError         string
	DueDateError       string
	CloseReasonError   string
	MemberOptions      []model.User
	SprintOptions      []uiIssueSprintOption
	SubIssues          []model.Issue
	SubIssuesHasMore   bool
	AddSubIssue        bool
	SubIssueTitle      string
	SubIssueError      string
	Comments           []uiIssueCommentItem
	CommentsHasMore    bool
	CommentBody        string
	CommentError       string
	EditCommentID      uuid.UUID
	CommentEditBody    string
	CommentEditError   string
	Links              []uiIssueLinkItem
	LinksHasMore       bool
	AddLink            bool
	EditLinkID         uuid.UUID
	LinkTarget         string
	LinkRelation       string
	LinkError          string
	Contexts           []model.ProjectContext
	ContextsHasMore    bool
	EditTags           bool
	TagModalAttached   []model.IssueTag
	TagModalAvailable  []model.IssueTag
	TagInput           string
	TagError           string
	BackHref           string
	BackHXGet          string
	BackLabel          string
	DeleteNotice       *uiIssueDeleteNotice
}

type uiTagManagerData struct {
	Mode        string
	Project     model.Project
	Issue       model.Issue
	HasIssue    bool
	CanWrite    bool
	Tags        []model.IssueTag
	Available   []model.IssueTag
	BackHref    string
	BackHXGet   string
	BackLabel   string
	NameInput   string
	ColorInput  model.IssueTagColor
	TagError    string
	EditTagID   uuid.UUID
	EditName    string
	EditColor   model.IssueTagColor
	EditError   string
	AttachInput string
	AttachError string
}

type uiProjectsPanelData struct {
	Projects []model.Project
	HasMore  bool
}

type uiNewProjectPanelData struct {
	Error       string
	Key         string
	Name        string
	Description string
}

type uiNewIssuePanelData struct {
	Project           model.Project
	HasProject        bool
	ProjectID         string
	ProjectInput      string
	ProjectSearchOpen bool
	ProjectOptions    []model.Project
	Error             string
	Title             string
	Description       string
	Priority          string
	DueDate           string
	AssigneeInput     string
	ReporterInput     string
	MemberOptions     []model.User
	BackHref          string
	BackHXGet         string
}

type uiTokenPanelData struct {
	Tokens  []model.AuthToken
	Error   string
	Created string
}

type uiSettingsPanelData struct {
	User            model.User
	ProfileError    string
	ProfileSaved    bool
	PasswordError   string
	PasswordChanged bool
	PasswordLogin   model.PasswordLoginState
	Passkeys        []model.PasskeyCredential
}
