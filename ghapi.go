package devstats

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

// IssueConfig - holds issue data
type IssueConfig struct {
	Repo         string
	Number       int
	IssueID      int64
	Pr           bool
	MilestoneID  *int64
	Labels       string
	LabelsMap    map[int64]string
	GhIssue      *github.Issue
	CreatedAt    time.Time
	EventID      int64
	EventType    string
	GhEvent      *github.IssueEvent
	AssigneeID   *int64
	Assignees    string
	AssigneesMap map[int64]string
}

func (ic IssueConfig) String() string {
	var (
		milestoneID int64
		assigneeID  int64
	)
	if ic.MilestoneID != nil {
		milestoneID = *ic.MilestoneID
	}
	if ic.AssigneeID != nil {
		assigneeID = *ic.AssigneeID
	}
	return fmt.Sprintf(
		"{Repo: %s, Number: %d, IssueID: %d, EventID: %d, EventType: %s, Pr: %v, MilestoneID: %d, AssigneeID: %d, CreatedAt: %s, Labels: %s, LabelsMap: %+v, Assignees: %s, AssigneesMap: %+v}",
		ic.Repo,
		ic.Number,
		ic.IssueID,
		ic.EventID,
		ic.EventType,
		ic.Pr,
		milestoneID,
		assigneeID,
		ToYMDHMSDate(ic.CreatedAt),
		ic.Labels,
		ic.LabelsMap,
		ic.Assignees,
		ic.AssigneesMap,
	)
}

// outputIssuesInfo: display summary of data to process
func outputIssuesInfo(issues map[int64]IssueConfigAry, info string) {
	data := make(map[string][]string)
	for _, cfgAry := range issues {
		for _, cfg := range cfgAry {
			key := fmt.Sprintf("%s %d", cfg.Repo, cfg.Number)
			val := fmt.Sprintf("%s %s", ToYMDHMSDate(cfg.CreatedAt), cfg.EventType)
			_, ok := data[key]
			if ok {
				data[key] = append(data[key], val)
			} else {
				data[key] = []string{val}
			}
		}
	}
	keys := []string{}
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	Printf("%s\n", info)
	for _, key := range keys {
		values := data[key]
		svalues := []string{}
		for _, value := range values {
			svalues = append(svalues, value)
		}
		sort.Strings(svalues)
		Printf("%s: [%s]\n", key, strings.Join(svalues, ", "))
	}
}

// IssueConfigAry - allows sorting IssueConfig array by IssueID annd then event creation date
type IssueConfigAry []IssueConfig

func (ic IssueConfigAry) Len() int      { return len(ic) }
func (ic IssueConfigAry) Swap(i, j int) { ic[i], ic[j] = ic[j], ic[i] }
func (ic IssueConfigAry) Less(i, j int) bool {
	if ic[i].IssueID != ic[j].IssueID {
		return ic[i].IssueID < ic[j].IssueID
	}
	if ic[i].CreatedAt != ic[j].CreatedAt {
		return ic[i].CreatedAt.Before(ic[j].CreatedAt)
	}
	return ic[i].EventID < ic[j].EventID
}

// GetRateLimits - returns all and remaining API points and duration to wait for reset
// when core=true - returns Core limits, when core=false returns Search limits
func GetRateLimits(gctx context.Context, gc *github.Client, core bool) (int, int, time.Duration) {
	rl, _, err := gc.RateLimits(gctx)
	if err != nil {
		Printf("GetRateLimit: %v\n", err)
	}
	if rl == nil {
		return -1, -1, time.Duration(5) * time.Second
	}
	if core {
		return rl.Core.Limit, rl.Core.Remaining, rl.Core.Reset.Time.Sub(time.Now()) + time.Duration(1)*time.Second
	}
	return rl.Search.Limit, rl.Search.Remaining, rl.Search.Reset.Time.Sub(time.Now()) + time.Duration(1)*time.Second
}

// GHClient - get GitHub client
func GHClient(ctx *Ctx) (ghCtx context.Context, client *github.Client) {
	// Get GitHub OAuth from env or from file
	oAuth := ctx.GitHubOAuth
	if strings.Contains(ctx.GitHubOAuth, "/") {
		bytes, err := ReadFile(ctx, ctx.GitHubOAuth)
		FatalOnError(err)
		oAuth = strings.TrimSpace(string(bytes))
	}

	// GitHub authentication or use public access
	ghCtx = context.Background()
	if oAuth == "-" {
		client = github.NewClient(nil)
	} else {
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: oAuth},
		)
		tc := oauth2.NewClient(ghCtx, ts)
		client = github.NewClient(tc)
	}
	return
}

// HandlePossibleError - display error specific message, detect rate limit and abuse
func HandlePossibleError(err error, cfg *IssueConfig, info string) string {
	if err != nil {
		_, rate := err.(*github.RateLimitError)
		_, abuse := err.(*github.AbuseRateLimitError)
		if abuse || rate {
			if rate {
				Printf("Rate limit (%s) for %v\n", info, cfg)
				return "rate"
			}
			if abuse {
				Printf("Abuse detected (%s) for %v\n", info, cfg)
				return Abuse
			}
		}
		if strings.Contains(err.Error(), "404 Not Found") {
			Printf("Not found (%s) for %v: %v\n", info, cfg, err)
			return NotFound
		}
		//FatalOnError(err)
		Printf("%s error: %v, non fatal, exiting 0 status\n", os.Args[0], err)
		os.Exit(0)
	}
	return ""
}

func ghActorIDOrNil(actPtr *github.User) interface{} {
	if actPtr == nil {
		return nil
	}
	return actPtr.ID
}

func ghActorLoginOrNil(actPtr *github.User, maybeHide func(string) string) interface{} {
	if actPtr == nil {
		return nil
	}
	if actPtr.Login == nil {
		return nil
	}
	return maybeHide(*actPtr.Login)
}

func ghMilestoneIDOrNil(milPtr *github.Milestone) interface{} {
	if milPtr == nil {
		return nil
	}
	return milPtr.ID
}

// Inserts single GitHub User
func ghActor(con *sql.Tx, ctx *Ctx, actor *github.User, maybeHide func(string) string) {
	if actor == nil || actor.Login == nil {
		return
	}
	ExecSQLTxWithErr(
		con,
		ctx,
		InsertIgnore("into gha_actors(id, login, name) "+NValues(3)),
		AnyArray{actor.ID, maybeHide(*actor.Login), ""}...,
	)
}

// Insert single GitHub milestone
func ghMilestone(con *sql.Tx, ctx *Ctx, eid int64, ic *IssueConfig, maybeHide func(string) string) {
	milestone := ic.GhIssue.Milestone
	ev := ic.GhEvent
	// gha_milestones
	ExecSQLTxWithErr(
		con,
		ctx,
		InsertIgnore(
			fmt.Sprintf(
				"into gha_milestones("+
					"id, event_id, closed_at, closed_issues, created_at, creator_id, "+
					"description, due_on, number, open_issues, state, title, updated_at, "+
					"dup_actor_id, dup_actor_login, dup_repo_id, dup_repo_name, dup_type, dup_created_at, "+
					"dupn_creator_login) values("+
					"%s, %s, %s, %s, %s, %s, "+
					"%s, %s, %s, %s, %s, %s, %s, "+
					"%s, %s, (select max(id) from gha_repos where name = %s), %s, %s, %s, "+
					"%s)",
				NValue(1),
				NValue(2),
				NValue(3),
				NValue(4),
				NValue(5),
				NValue(6),
				NValue(7),
				NValue(8),
				NValue(9),
				NValue(10),
				NValue(11),
				NValue(12),
				NValue(13),
				NValue(14),
				NValue(15),
				NValue(16),
				NValue(17),
				NValue(18),
				NValue(19),
				NValue(20),
			),
		),
		AnyArray{
			ic.MilestoneID,
			eid,
			milestone.ClosedAt,
			milestone.ClosedIssues,
			milestone.CreatedAt,
			ghActorIDOrNil(milestone.Creator),
			TruncStringOrNil(milestone.Description, 0xffff),
			milestone.DueOn,
			milestone.Number,
			milestone.OpenIssues,
			milestone.State,
			TruncStringOrNil(milestone.Title, 200),
			milestone.UpdatedAt,
			ev.Actor.ID,
			maybeHide(*ev.Actor.Login),
			ic.Repo,
			ic.Repo,
			ic.EventType,
			ic.CreatedAt,
			ghActorLoginOrNil(milestone.Creator, maybeHide),
		}...,
	)
}

// GetRecentRepos - get list of repos active last day
func GetRecentRepos(c *sql.DB, ctx *Ctx, dtFrom time.Time) (repos []string) {
	rows := QuerySQLWithErr(
		c,
		ctx,
		fmt.Sprintf(
			"select distinct dup_repo_name from gha_events "+
				"where created_at > %s",
			NValue(1),
		),
		dtFrom,
	)
	defer func() { FatalOnError(rows.Close()) }()
	var repo string
	for rows.Next() {
		FatalOnError(rows.Scan(&repo))
		repos = append(repos, repo)
	}
	FatalOnError(rows.Err())
	return
}

// ArtificialPREvent - create artificial API event (PR state for now())
func ArtificialPREvent(c *sql.DB, ctx *Ctx, cfg *IssueConfig, pr *github.PullRequest) (err error) {
	if ctx.SkipPDB {
		if ctx.Debug > 0 {
			Printf("No DB write: PR '%v'\n", *cfg)
		}
		return nil
	}
	// To handle GDPR
	maybeHide := MaybeHideFunc(GetHidden(HideCfgFile))

	eventID := 281474976710656 + cfg.EventID
	eType := cfg.EventType
	eCreatedAt := cfg.CreatedAt
	event := cfg.GhEvent
	issue := cfg.GhIssue
	iid := *issue.ID
	actor := cfg.GhEvent.Actor

	// Start transaction
	tc, err := c.Begin()
	FatalOnError(err)

	// User
	ghActor(tc, ctx, pr.User, maybeHide)

	baseSHA := ""
	headSHA := ""
	if pr.Base != nil && pr.Base.SHA != nil {
		baseSHA = *pr.Base.SHA
	}
	if pr.Head != nil && pr.Head.SHA != nil {
		headSHA = *pr.Head.SHA
	}

	if pr.MergedBy != nil {
		ghActor(tc, ctx, pr.MergedBy, maybeHide)
	}

	if pr.Assignee != nil {
		ghActor(tc, ctx, pr.Assignee, maybeHide)
	}

	if pr.Milestone != nil {
		ghMilestone(tc, ctx, eventID, cfg, maybeHide)
	}

	prid := *pr.ID
	ExecSQLTxWithErr(
		tc,
		ctx,
		fmt.Sprintf(
			"insert into gha_pull_requests("+
				"id, event_id, user_id, base_sha, head_sha, merged_by_id, assignee_id, milestone_id, "+
				"number, state, title, body, created_at, updated_at, closed_at, merged_at, "+
				"merge_commit_sha, merged, mergeable, mergeable_state, comments, "+
				"maintainer_can_modify, commits, additions, deletions, changed_files, "+
				"dup_actor_id, dup_actor_login, dup_repo_id, dup_repo_name, dup_type, dup_created_at, "+
				"dup_user_login, dupn_assignee_login, dupn_merged_by_login) values("+
				"%s, %s, %s, %s, %s, %s, %s, %s, "+
				"%s, %s, %s, %s, %s, %s, %s, %s, "+
				"%s, %s, %s, %s, %s, "+
				"%s, %s, %s, %s, %s, "+
				"%s, %s, (select max(id) from gha_repos where name = %s), %s, %s, %s, "+
				"%s, %s, %s)",
			NValue(1),
			NValue(2),
			NValue(3),
			NValue(4),
			NValue(5),
			NValue(6),
			NValue(7),
			NValue(8),
			NValue(9),
			NValue(10),
			NValue(11),
			NValue(12),
			NValue(13),
			NValue(14),
			NValue(15),
			NValue(16),
			NValue(17),
			NValue(18),
			NValue(19),
			NValue(20),
			NValue(21),
			NValue(22),
			NValue(23),
			NValue(24),
			NValue(25),
			NValue(26),
			NValue(27),
			NValue(28),
			NValue(29),
			NValue(30),
			NValue(31),
			NValue(32),
			NValue(33),
			NValue(34),
			NValue(35),
		),
		AnyArray{
			prid,
			eventID,
			ghActorIDOrNil(pr.User),
			baseSHA,
			headSHA,
			ghActorIDOrNil(pr.MergedBy),
			ghActorIDOrNil(pr.Assignee),
			ghMilestoneIDOrNil(pr.Milestone),
			pr.Number,
			pr.State,
			pr.Title,
			TruncStringOrNil(pr.Body, 0xffff),
			pr.CreatedAt,
			pr.UpdatedAt,
			TimeOrNil(pr.ClosedAt),
			TimeOrNil(pr.MergedAt),
			StringOrNil(pr.MergeCommitSHA),
			BoolOrNil(pr.Merged),
			BoolOrNil(pr.Mergeable),
			StringOrNil(pr.MergeableState),
			IntOrNil(pr.Comments),
			BoolOrNil(pr.MaintainerCanModify),
			IntOrNil(pr.Commits),
			IntOrNil(pr.Additions),
			IntOrNil(pr.Deletions),
			IntOrNil(pr.ChangedFiles),
			actor.ID,
			ghActorLoginOrNil(actor, maybeHide),
			cfg.Repo,
			cfg.Repo,
			eType,
			eCreatedAt,
			ghActorLoginOrNil(pr.User, maybeHide),
			ghActorLoginOrNil(pr.Assignee, maybeHide),
			ghActorLoginOrNil(pr.MergedBy, maybeHide),
		}...,
	)

	// Create artificial event
	ExecSQLTxWithErr(
		tc,
		ctx,
		InsertIgnore(
			fmt.Sprintf(
				"into gha_events("+
					"id, type, actor_id, repo_id, public, created_at, "+
					"dup_actor_login, dup_repo_name, org_id, forkee_id) "+
					"values(%s, %s, %s, (select max(id) from gha_repos where name = %s), true, %s, "+
					"%s, %s, (select max(org_id) from gha_repos where name = %s), null)",
				NValue(1),
				NValue(2),
				NValue(3),
				NValue(4),
				NValue(5),
				NValue(6),
				NValue(7),
				NValue(8),
			),
		),
		AnyArray{
			eventID,
			cfg.EventType,
			ghActorIDOrNil(event.Actor),
			cfg.Repo,
			eCreatedAt,
			ghActorLoginOrNil(event.Actor, maybeHide),
			cfg.Repo,
			cfg.Repo,
		}...,
	)

	// Create artificial event's payload
	ExecSQLTxWithErr(
		tc,
		ctx,
		InsertIgnore(
			fmt.Sprintf(
				"into gha_payloads("+
					"event_id, push_id, size, ref, head, befor, action, "+
					"issue_id, pull_request_id, comment_id, ref_type, master_branch, commit, "+
					"description, number, forkee_id, release_id, member_id, "+
					"dup_actor_id, dup_actor_login, dup_repo_id, dup_repo_name, dup_type, dup_created_at) "+
					"values(%s, null, null, null, null, null, %s, "+
					"%s, %s, null, null, null, null, "+
					"null, %s, null, null, null, "+
					"%s, %s, (select max(id) from gha_repos where name = %s), %s, %s, %s)",
				NValue(1),
				NValue(2),
				NValue(3),
				NValue(4),
				NValue(5),
				NValue(6),
				NValue(7),
				NValue(8),
				NValue(9),
				NValue(10),
				NValue(11),
			),
		),
		AnyArray{
			eventID,
			cfg.EventType,
			iid,
			prid,
			issue.Number,
			ghActorIDOrNil(event.Actor),
			ghActorLoginOrNil(event.Actor, maybeHide),
			cfg.Repo,
			cfg.Repo,
			cfg.EventType,
			eCreatedAt,
		}...,
	)

	// If such payload already existed, we need to set PR ID on it
	ExecSQLTxWithErr(
		tc,
		ctx,
		fmt.Sprintf(
			"update gha_payloads set pull_request_id = %s where issue_id = %s and event_id = %s",
			NValue(1),
			NValue(2),
			NValue(3),
		),
		AnyArray{prid, iid, eventID}...,
	)

	// Arrays: actors: assignees, requested_reviewers
	// assignees

	if pr.Assignees != nil {
		for _, assignee := range pr.Assignees {
			if assignee == nil {
				continue
			}
			// assignee
			ghActor(tc, ctx, assignee, maybeHide)

			ExecSQLTxWithErr(
				tc,
				ctx,
				"insert into gha_pull_requests_assignees(pull_request_id, event_id, assignee_id) "+NValues(3),
				AnyArray{prid, eventID, assignee.ID}...,
			)
		}
	}

	// requested_reviewers
	if pr.RequestedReviewers != nil {
		for _, reviewer := range pr.RequestedReviewers {
			if reviewer == nil {
				continue
			}
			// reviewer
			ghActor(tc, ctx, reviewer, maybeHide)

			// pull_request-requested_reviewer connection
			ExecSQLTxWithErr(
				tc,
				ctx,
				"insert into gha_pull_requests_requested_reviewers(pull_request_id, event_id, requested_reviewer_id) "+NValues(3),
				AnyArray{prid, eventID, reviewer.ID}...,
			)
		}
	}
	// Final commit
	FatalOnError(tc.Commit())
	//FatalOnError(tc.Rollback())
	return
}

// ArtificialEvent - create artificial API event (but from the past)
func ArtificialEvent(c *sql.DB, ctx *Ctx, cfg *IssueConfig) (err error) {
	// github.com/google/go-github/github/issues_events.go
	if ctx.SkipPDB {
		if ctx.Debug > 0 {
			Printf("No DB write: Issue '%v'\n", *cfg)
		}
		return nil
	}
	// Create artificial event, add 2^48 to eid
	eid := cfg.EventID
	iid := cfg.IssueID
	issue := cfg.GhIssue
	event := cfg.GhEvent
	eventID := 281474976710656 + eid
	now := cfg.CreatedAt

	// To handle GDPR
	maybeHide := MaybeHideFunc(GetHidden(HideCfgFile))

	// Start transaction
	tc, err := c.Begin()
	FatalOnError(err)

	// Actors
	ghActor(tc, ctx, issue.Assignee, maybeHide)
	ghActor(tc, ctx, issue.User, maybeHide)
	for _, assignee := range issue.Assignees {
		ghActor(tc, ctx, assignee, maybeHide)
	}
	if issue.Milestone != nil {
		ghActor(tc, ctx, issue.Milestone.Creator, maybeHide)
	}

	// Create new issue state
	ExecSQLTxWithErr(
		tc,
		ctx,
		fmt.Sprintf(
			"insert into gha_issues("+
				"id, event_id, assignee_id, body, closed_at, comments, created_at, "+
				"locked, milestone_id, number, state, title, updated_at, user_id, "+
				"dup_actor_id, dup_actor_login, dup_repo_id, dup_repo_name, dup_type, dup_created_at, "+
				"dup_user_login, dupn_assignee_login, is_pull_request) "+
				"values(%s, %s, %s, %s, %s, %s, %s, "+
				"%s, %s, %s, %s, %s, %s, %s, "+
				"%s, %s, (select max(id) from gha_repos where name = %s), %s, %s, %s, "+
				"%s, %s, %s) ",
			NValue(1),
			NValue(2),
			NValue(3),
			NValue(4),
			NValue(5),
			NValue(6),
			NValue(7),
			NValue(8),
			NValue(9),
			NValue(10),
			NValue(11),
			NValue(12),
			NValue(13),
			NValue(14),
			NValue(15),
			NValue(16),
			NValue(17),
			NValue(18),
			NValue(19),
			NValue(20),
			NValue(21),
			NValue(22),
			NValue(23),
		),
		AnyArray{
			iid,
			eventID,
			ghActorIDOrNil(issue.Assignee),
			TruncStringOrNil(issue.Body, 0xffff),
			TimeOrNil(issue.ClosedAt),
			IntOrNil(issue.Comments),
			issue.CreatedAt,
			BoolOrNil(issue.Locked),
			ghMilestoneIDOrNil(issue.Milestone),
			issue.Number,
			issue.State,
			issue.Title,
			now,
			ghActorIDOrNil(issue.User),
			ghActorIDOrNil(event.Actor),
			ghActorLoginOrNil(event.Actor, maybeHide),
			cfg.Repo,
			cfg.Repo,
			cfg.EventType,
			now,
			ghActorLoginOrNil(issue.User, maybeHide),
			ghActorLoginOrNil(issue.Assignee, maybeHide),
			issue.IsPullRequest(),
		}...,
	)

	// Create Milestone if new event and milestone non-null
	if issue.Milestone != nil {
		ghMilestone(tc, ctx, eventID, cfg, maybeHide)
	}

	// Create artificial event
	ExecSQLTxWithErr(
		tc,
		ctx,
		InsertIgnore(
			fmt.Sprintf(
				"into gha_events("+
					"id, type, actor_id, repo_id, public, created_at, "+
					"dup_actor_login, dup_repo_name, org_id, forkee_id) "+
					"values(%s, %s, %s, (select max(id) from gha_repos where name = %s), true, %s, "+
					"%s, %s, (select max(org_id) from gha_repos where name = %s), null)",
				NValue(1),
				NValue(2),
				NValue(3),
				NValue(4),
				NValue(5),
				NValue(6),
				NValue(7),
				NValue(8),
			),
		),
		AnyArray{
			eventID,
			cfg.EventType,
			ghActorIDOrNil(event.Actor),
			cfg.Repo,
			now,
			ghActorLoginOrNil(event.Actor, maybeHide),
			cfg.Repo,
			cfg.Repo,
		}...,
	)

	// Create artificial event's payload
	ExecSQLTxWithErr(
		tc,
		ctx,
		InsertIgnore(
			fmt.Sprintf(
				"into gha_payloads("+
					"event_id, push_id, size, ref, head, befor, action, "+
					"issue_id, pull_request_id, comment_id, ref_type, master_branch, commit, "+
					"description, number, forkee_id, release_id, member_id, "+
					"dup_actor_id, dup_actor_login, dup_repo_id, dup_repo_name, dup_type, dup_created_at) "+
					"values(%s, null, null, null, null, null, %s, "+
					"%s, null, null, null, null, null, "+
					"null, %s, null, null, null, "+
					"%s, %s, (select max(id) from gha_repos where name = %s), %s, %s, %s)",
				NValue(1),
				NValue(2),
				NValue(3),
				NValue(4),
				NValue(5),
				NValue(6),
				NValue(7),
				NValue(8),
				NValue(9),
				NValue(10),
			),
		),
		AnyArray{
			eventID,
			cfg.EventType,
			iid,
			issue.Number,
			ghActorIDOrNil(event.Actor),
			ghActorLoginOrNil(event.Actor, maybeHide),
			cfg.Repo,
			cfg.Repo,
			cfg.EventType,
			now,
		}...,
	)

	// Add issue labels
	for labelID, labelName := range cfg.LabelsMap {
		ExecSQLTxWithErr(
			tc,
			ctx,
			fmt.Sprintf(
				"insert into gha_issues_labels(issue_id, event_id, label_id, "+
					"dup_actor_id, dup_actor_login, dup_repo_id, dup_repo_name, "+
					"dup_type, dup_created_at, dup_issue_number, dup_label_name) "+
					"values(%s, %s, %s, "+
					"%s, %s, (select max(id) from gha_repos where name = %s), %s, "+
					"%s, %s, %s, %s)",
				NValue(1),
				NValue(2),
				NValue(3),
				NValue(4),
				NValue(5),
				NValue(6),
				NValue(7),
				NValue(8),
				NValue(9),
				NValue(10),
				NValue(11),
			),
			AnyArray{
				iid,
				eventID,
				labelID,
				ghActorIDOrNil(event.Actor),
				ghActorLoginOrNil(event.Actor, maybeHide),
				cfg.Repo,
				cfg.Repo,
				cfg.EventType,
				now,
				issue.Number,
				labelName,
			}...,
		)
	}

	// Add issue assignees
	for assigneeID := range cfg.AssigneesMap {
		ExecSQLTxWithErr(
			tc,
			ctx,
			fmt.Sprintf(
				"insert into gha_issues_assignees(issue_id, event_id, assignee_id) "+
					"values(%s, %s, %s)",
				NValue(1),
				NValue(2),
				NValue(3),
			),
			AnyArray{
				iid,
				eventID,
				assigneeID,
			}...,
		)
	}

	// Final commit
	FatalOnError(tc.Commit())
	//FatalOnError(tc.Rollback())
	return
}

// SyncIssuesState synchonizes issues states
// manual:
//  false: normal devstats sync cron mode using 'ghapi2db' tool
//  true: manual sync using'sync_issues' tool
func SyncIssuesState(gctx context.Context, gc *github.Client, ctx *Ctx, c *sql.DB, issues map[int64]IssueConfigAry, prs map[int64]github.PullRequest, manual bool) {
	nIssuesBefore := 0
	for _, issueConfig := range issues {
		nIssuesBefore += len(issueConfig)
	}

	// Make sure we only have single event per single second - final state with highest EventID that was sorted
	// Sort by iid then created_at then event_id
	// in manual mode we only have one entry per issue so no sort is needed
	if !manual {
		for issueID := range issues {
			sort.Sort(issues[issueID])
			if ctx.Debug > 1 {
				Printf("Sorted: %+v\n", issues[issueID])
			}
		}
		// Leave only final state
		for iid, issueConfigAry := range issues {
			mp := make(map[string]IssueConfig)
			for _, issue := range issueConfigAry {
				sdt := ToYMDHMSDate(issue.CreatedAt)
				mp[sdt] = issue
			}
			sdts := []string{}
			for sdt := range mp {
				sdts = append(sdts, sdt)
			}
			sort.Strings(sdts)
			issues[iid] = []IssueConfig{}
			for _, sdt := range sdts {
				issues[iid] = append(issues[iid], mp[sdt])
			}
		}
	}

	// Output data info
	outputIssuesInfo(issues, "Issues to process")

	// Get number of CPUs available
	thrN := GetThreadsNum(ctx)

	var issuesMutex = &sync.RWMutex{}
	// Now iterate all issues/PR in MT mode
	ch := make(chan bool)
	nThreads := 0
	dtStart := time.Now()
	lastTime := dtStart
	nIssues := 0
	for _, issueConfig := range issues {
		nIssues += len(issueConfig)
	}
	nPRs := len(prs)
	checked := 0
	var updatesMutex = &sync.Mutex{}
	updates := []int{0, 0, 0, 0}
	// updates (non-manual mode):
	// 0: no such event (for exact date) --> new
	// 1: artificial exists (for exact date) --> skip
	// 2: normal exists (for exact date) --> no new
	// 3: normal exists (for exact date) --> new needed
	// updates (manual mode)
	// 0 - no such issue --> new
	// 2: previous issue state exists, no new
	// 3: previous issue state exists, new needed
	why := ""
	what := ""
	// infos uses updatesmutex (when updates is also changed)
	// when only infos is changes it uses infoxMutex
	var infosMutex = &sync.Mutex{}
	infos := make(map[string][]string)

	Printf("ghapi2db.go: Processing %d PRs, %d issues (%d with date collisions), manual mode: %v - GHA part\n", nPRs, nIssues, nIssuesBefore, manual)
	// Use map key to pass to the closure
	for key, issueConfig := range issues {
		for idx := range issueConfig {
			go func(ch chan bool, iid int64, idx int) {
				// Refer to current tag using index passed to anonymous function
				issuesMutex.RLock()
				cfg := issues[iid][idx]
				issuesMutex.RUnlock()
				if ctx.Debug > 1 {
					Printf("GHA Issue ID '%d' --> '%v'\n", iid, cfg)
				}
				var (
					ghaMilestoneID *int64
					ghaEventID     int64
					ghaClosedAt    *time.Time
					ghaState       string
					ghaTitle       string
					ghaLocked      bool
					ghaAssigneeID  *int64
				)

				// Process current milestone (given issue and second)
				apiMilestoneID := cfg.MilestoneID
				apiClosedAt := cfg.GhIssue.ClosedAt
				apiState := *cfg.GhIssue.State
				apiTitle := *cfg.GhIssue.Title
				apiLocked := *cfg.GhIssue.Locked
				apiAssigneeID := cfg.AssigneeID
				var rowsM *sql.Rows
				if manual {
					rowsM = QuerySQLWithErr(
						c,
						ctx,
						fmt.Sprintf(
							"select milestone_id, event_id, closed_at, state, title, locked, assignee_id "+
								"from gha_issues where id = %s "+
								"order by updated_at desc, event_id desc limit 1",
							NValue(1),
						),
						cfg.IssueID,
					)
				} else {
					rowsM = QuerySQLWithErr(
						c,
						ctx,
						fmt.Sprintf(
							"select milestone_id, event_id, closed_at, state, title, locked, assignee_id "+
								"from gha_issues where id = %s and updated_at = %s "+
								"order by updated_at desc, event_id desc limit 1",
							NValue(1),
							NValue(2),
						),
						cfg.IssueID,
						cfg.CreatedAt,
					)
				}
				defer func() { FatalOnError(rowsM.Close()) }()
				got := false
				for rowsM.Next() {
					FatalOnError(
						rowsM.Scan(
							&ghaMilestoneID,
							&ghaEventID,
							&ghaClosedAt,
							&ghaState,
							&ghaTitle,
							&ghaLocked,
							&ghaAssigneeID,
						),
					)
					got = true
				}
				FatalOnError(rowsM.Err())
				if !got {
					if ctx.Debug > 1 {
						Printf("Adding missing (%v) event '%v'\n", cfg.CreatedAt, cfg)
					}
					FatalOnError(
						ArtificialEvent(
							c,
							ctx,
							&cfg,
						),
					)
					if manual {
						why = "no previous issue state"
						what = fmt.Sprintf("%s %d", cfg.Repo, cfg.Number)
					} else {
						why = "no event at date"
						what = fmt.Sprintf("%s %d %s %s", cfg.Repo, cfg.Number, ToYMDHMSDate(cfg.CreatedAt), cfg.EventType)
					}
					updatesMutex.Lock()
					updates[0]++
					_, ok := infos[why]
					if ok {
						infos[why] = append(infos[why], what)
					} else {
						infos[why] = []string{what}
					}
					updatesMutex.Unlock()
					ch <- true
					return
				}
				// We have such artificial event and code is making sure it is most up-to-date for a given second, so we may skip it.
				if !manual && ghaEventID > 281474976710656 {
					if ctx.Debug > 1 {
						Printf("Artificial event (%v) already exists, skipping: '%v'\n", cfg.CreatedAt, cfg)
					}
					why = "already have artificial event at date"
					what = fmt.Sprintf("%s %d %s %s", cfg.Repo, cfg.Number, ToYMDHMSDate(cfg.CreatedAt), cfg.EventType)
					updatesMutex.Lock()
					updates[1]++
					_, ok := infos[why]
					if ok {
						infos[why] = append(infos[why], what)
					} else {
						infos[why] = []string{what}
					}
					updatesMutex.Unlock()
					ch <- false
					return
				}

				// Now have existing GHA event, but we don't know if it is a correct state event
				// Or just bot comment after which (on the same second) milestone or label(s) are updated
				// Check state change
				changedState := false
				if apiState != ghaState {
					changedState = true
					if ctx.Debug > 1 {
						Printf("Updating issue '%v' state %s -> %s\n", cfg, ghaState, apiState)
					}
					why = "changed issue state"
					if manual {
						what = fmt.Sprintf("%s %d: %s -> %s", cfg.Repo, cfg.Number, ghaState, apiState)
					} else {
						what = fmt.Sprintf("%s %d %s %s: %s -> %s", cfg.Repo, cfg.Number, ToYMDHMSDate(cfg.CreatedAt), cfg.EventType, ghaState, apiState)
					}
					infosMutex.Lock()
					_, ok := infos[why]
					if ok {
						infos[why] = append(infos[why], what)
					} else {
						infos[why] = []string{what}
					}
					infosMutex.Unlock()
				}

				// Check title change
				changedTitle := false
				if apiTitle != ghaTitle {
					changedTitle = true
					if ctx.Debug > 0 {
						Printf("Updating issue '%v' title %s -> %s\n", cfg, ghaTitle, apiTitle)
					}
					why = "changed issue title"
					if manual {
						what = fmt.Sprintf("%s %d: %s -> %s", cfg.Repo, cfg.Number, ghaTitle, apiTitle)
					} else {
						what = fmt.Sprintf("%s %d %s %s: %s -> %s", cfg.Repo, cfg.Number, ToYMDHMSDate(cfg.CreatedAt), cfg.EventType, ghaTitle, apiTitle)
					}
					infosMutex.Lock()
					_, ok := infos[why]
					if ok {
						infos[why] = append(infos[why], what)
					} else {
						infos[why] = []string{what}
					}
					infosMutex.Unlock()
				}

				// Check locked change
				changedLocked := false
				if apiLocked != ghaLocked {
					changedLocked = true
					if ctx.Debug > 0 {
						Printf("Updating issue '%v' locked %v -> %v\n", cfg, ghaLocked, apiLocked)
					}
				}

				// Check closed_at change
				changedClosed := false
				if (apiClosedAt == nil && ghaClosedAt != nil) || (apiClosedAt != nil && ghaClosedAt == nil) || (apiClosedAt != nil && ghaClosedAt != nil && ToYMDHMSDate(*apiClosedAt) != ToYMDHMSDate(*ghaClosedAt)) {
					changedClosed = true
					if ctx.Debug > 0 {
						from := Null
						if ghaClosedAt != nil {
							from = fmt.Sprintf("%v", ToYMDHMSDate(*ghaClosedAt))
						}
						to := Null
						if apiClosedAt != nil {
							to = fmt.Sprintf("%v", ToYMDHMSDate(*apiClosedAt))
						}
						Printf("Updating issue '%v' closed_at %s -> %s\n", cfg, from, to)
					}
				}

				// Check milestone change
				changedMilestone := false
				if (apiMilestoneID == nil && ghaMilestoneID != nil) || (apiMilestoneID != nil && ghaMilestoneID == nil) || (apiMilestoneID != nil && ghaMilestoneID != nil && *apiMilestoneID != *ghaMilestoneID) {
					changedMilestone = true
					if ctx.Debug > 0 {
						from := Null
						if ghaMilestoneID != nil {
							from = fmt.Sprintf("%d", *ghaMilestoneID)
						}
						to := Null
						if apiMilestoneID != nil {
							to = fmt.Sprintf("%d", *apiMilestoneID)
						}
						Printf("Updating issue '%v' milestone %s -> %s\n", cfg, from, to)
					}
				}

				// Check assignee change
				changedAssignee := false
				if (apiAssigneeID == nil && ghaAssigneeID != nil) || (apiAssigneeID != nil && ghaAssigneeID == nil) || (apiAssigneeID != nil && ghaAssigneeID != nil && *apiAssigneeID != *ghaAssigneeID) {
					changedAssignee = true
					if ctx.Debug > 0 {
						from := Null
						if ghaAssigneeID != nil {
							from = fmt.Sprintf("%d", *ghaAssigneeID)
						}
						to := Null
						if apiAssigneeID != nil {
							to = fmt.Sprintf("%d", *apiAssigneeID)
						}
						Printf("Updating issue '%v' assignee %s -> %s\n", cfg, from, to)
					}
				}

				// Process current labels
				rowsL := QuerySQLWithErr(
					c,
					ctx,
					fmt.Sprintf(
						"select coalesce(string_agg(sub.label_id::text, ','), '') from "+
							"(select label_id from gha_issues_labels where event_id = %s "+
							"order by label_id) sub",
						NValue(1),
					),
					ghaEventID,
				)
				defer func() { FatalOnError(rowsL.Close()) }()
				ghaLabels := ""
				for rowsL.Next() {
					FatalOnError(rowsL.Scan(&ghaLabels))
				}
				FatalOnError(rowsL.Err())
				changedLabels := false
				if ghaLabels != cfg.Labels {
					if ctx.Debug > 0 {
						Printf("Updating issue '%v' labels to '%s', they were: '%s' (event_id %d)\n", cfg, cfg.Labels, ghaLabels, ghaEventID)
					}
					changedLabels = true
				}

				// Process current assignees
				rowsA := QuerySQLWithErr(
					c,
					ctx,
					fmt.Sprintf(
						"select coalesce(string_agg(sub.assignee_id::text, ','), '') from "+
							"(select assignee_id from gha_issues_assignees where event_id = %s "+
							"order by assignee_id) sub",
						NValue(1),
					),
					ghaEventID,
				)
				defer func() { FatalOnError(rowsA.Close()) }()
				ghaAssignees := ""
				for rowsA.Next() {
					FatalOnError(rowsA.Scan(&ghaAssignees))
				}
				FatalOnError(rowsA.Err())
				changedAssignees := false
				if ghaAssignees != cfg.Assignees {
					if ctx.Debug > 0 {
						Printf("Updating issue '%v' assignees to '%s', they were: '%s' (event_id %d)\n", cfg, cfg.Assignees, ghaAssignees, ghaEventID)
					}
					changedAssignees = true
				}

				uidx := 2
				// Do the update if needed
				changedAnything := changedMilestone || changedState || changedClosed || changedAssignee || changedTitle || changedLocked || changedLabels || changedAssignees
				if changedAnything {
					uidx = 3
					FatalOnError(
						ArtificialEvent(
							c,
							ctx,
							&cfg,
						),
					)
				}

				if ctx.Debug > 0 {
					if manual {
						Printf("Previous event (event_id: %d), added artificial: %v: '%v'\n", ghaEventID, changedAnything, cfg)
					} else {
						Printf("Event for the same date (%v) exist (event_id: %d), added artificial: %v: '%v'\n", cfg.CreatedAt, ghaEventID, changedAnything, cfg)
					}
				}
				updatesMutex.Lock()
				updates[uidx]++
				updatesMutex.Unlock()
				// Synchronize go routine
				ch <- changedAnything
			}(ch, key, idx)

			// go routine called with 'ch' channel to sync and tag index
			nThreads++
			if nThreads == thrN {
				<-ch
				nThreads--
				checked++
				ProgressInfo(checked, nIssues, dtStart, &lastTime, time.Duration(10)*time.Second, "")
			}
		}
	}
	// Usually all work happens on '<-ch'
	for nThreads > 0 {
		<-ch
		nThreads--
		checked++
		ProgressInfo(checked, nIssues, dtStart, &lastTime, time.Duration(10)*time.Second, "")
	}
	// Get RateLimits info
	_, rem, wait := GetRateLimits(gctx, gc, true)
	if manual {
		Printf(
			"ghapi2db.go: Manually processed %d issues/PRs (%d new issues, existing: %d not needed, %d added): %d API points remain, resets in %v\n",
			checked, updates[0], updates[2], updates[3], rem, wait,
		)
	} else {
		Printf(
			"ghapi2db.go: Automatically processed %d issues/PRs (%d new for date, %d artificial exists, date exists: %d not needed, %d added): %d API points remain, resets in %v\n",
			checked, updates[0], updates[1], updates[2], updates[3], rem, wait,
		)
	}

	// PRs sync (using state at run date XX:08+)
	// Use map key to pass to the closure
	ch = make(chan bool)
	nThreads = 0
	dtStart = time.Now()
	lastTime = dtStart
	checked = 0
	updates = []int{0, 0, 0, 0, 0}
	// updates[4] - collisions (only with non-manual mode)
	var prsMutex = &sync.RWMutex{}
	for iid := range prs {
		go func(ch chan bool, iid int64) {
			prsMutex.RLock()
			pr := prs[iid]
			ica := issues[iid]
			l := len(ica)
			ic := ica[l-1]
			prsMutex.RUnlock()
			prid := *pr.ID
			updatedAt := *pr.UpdatedAt
			if ctx.Debug > 0 {
				Printf("GHA Issue ID '%d' --> PR ID %d, updated %v\n", iid, prid, updatedAt)
			}
			var (
				ghaMilestoneID *int64
				ghaEventID     int64
				ghaClosedAt    *time.Time
				ghaState       string
				ghaTitle       string
				ghaMergedByID  *int64
				ghaMergedAt    *time.Time
				ghaMerged      *bool
				ghaAssigneeID  *int64
				apiMilestoneID *int64
				apiAssigneeID  *int64
				apiMergedByID  *int64
			)

			// Process current milestone
			if pr.Milestone != nil {
				apiMilestoneID = pr.Milestone.ID
			}
			apiClosedAt := pr.ClosedAt
			apiState := *pr.State
			apiTitle := *pr.Title
			if pr.Assignee != nil {
				apiAssigneeID = pr.Assignee.ID
			}
			if pr.MergedBy != nil {
				apiMergedByID = pr.MergedBy.ID
			}
			apiMergedAt := pr.MergedAt
			apiMerged := pr.Merged

			// Handle eventual collision
			if !manual {
				eventID := 281474976710656 + ic.EventID
				rowsE := QuerySQLWithErr(
					c,
					ctx,
					fmt.Sprintf(
						"select 1 from gha_pull_requests where id = %s and event_id = %s and updated_at != %s",
						NValue(1),
						NValue(2),
						NValue(3),
					),
					prid,
					eventID,
					updatedAt,
				)
				defer func() { FatalOnError(rowsE.Close()) }()
				collision := false
				dummy := 0
				for rowsE.Next() {
					FatalOnError(rowsE.Scan(&dummy))
					collision = true
				}
				FatalOnError(rowsE.Err())
				if collision {
					if ctx.Debug > 0 {
						Printf("Exact PR event already exists, skipping: '%v', PR ID: %d\n", updatedAt, prid)
					}
					updatesMutex.Lock()
					updates[4]++
					updatesMutex.Unlock()
					ch <- false
					return
				}
			}

			// Get event for this date
			var rowsM *sql.Rows
			if manual {
				rowsM = QuerySQLWithErr(
					c,
					ctx,
					fmt.Sprintf(
						"select milestone_id, event_id, closed_at, state, title, assignee_id, "+
							"merged_by_id, merged_at, merged "+
							"from gha_pull_requests where id = %s "+
							"order by updated_at desc, event_id desc limit 1",
						NValue(1),
					),
					prid,
				)
			} else {
				rowsM = QuerySQLWithErr(
					c,
					ctx,
					fmt.Sprintf(
						"select milestone_id, event_id, closed_at, state, title, assignee_id, "+
							"merged_by_id, merged_at, merged "+
							"from gha_pull_requests where id = %s and updated_at = %s "+
							"order by updated_at desc, event_id desc limit 1",
						NValue(1),
						NValue(2),
					),
					prid,
					updatedAt,
				)
			}
			defer func() { FatalOnError(rowsM.Close()) }()
			got := false
			for rowsM.Next() {
				FatalOnError(
					rowsM.Scan(
						&ghaMilestoneID,
						&ghaEventID,
						&ghaClosedAt,
						&ghaState,
						&ghaTitle,
						&ghaAssigneeID,
						&ghaMergedByID,
						&ghaMergedAt,
						&ghaMerged,
					),
				)
				got = true
			}
			FatalOnError(rowsM.Err())
			if !got {
				if ctx.Debug > 0 {
					Printf("Adding missing (%v) PR event '%v', PR ID: %d\n", updatedAt, ic, prid)
				}
				FatalOnError(
					ArtificialPREvent(
						c,
						ctx,
						&ic,
						&pr,
					),
				)
				updatesMutex.Lock()
				updates[0]++
				updatesMutex.Unlock()
				ch <- true
				return
			}
			if !manual && ghaEventID > 281474976710656 {
				if ctx.Debug > 0 {
					Printf("Artificial PR event (%v) already exists, skipping: '%v', PR ID: %d\n", updatedAt, ic, prid)
				}
				updatesMutex.Lock()
				updates[1]++
				updatesMutex.Unlock()
				ch <- false
				return
			}

			// Check state change
			changedState := false
			if apiState != ghaState {
				changedState = true
				if ctx.Debug > 0 {
					Printf("Updating PR '%v' state %s -> %s\n", ic, ghaState, apiState)
				}
			}

			// Check title change
			changedTitle := false
			if apiTitle != ghaTitle {
				changedTitle = true
				if ctx.Debug > 0 {
					Printf("Updating PR '%v' title %s -> %s\n", ic, ghaTitle, apiTitle)
				}
			}

			// Check merged change
			changedMerged := false
			if (apiMerged == nil && ghaMerged != nil) || (apiMerged != nil && ghaMerged == nil) || (apiMerged != nil && ghaMerged != nil && *apiMerged != *ghaMerged) {
				changedMerged = true
				if ctx.Debug > 0 {
					from := Null
					if ghaMerged != nil {
						from = fmt.Sprintf("%v", *ghaMerged)
					}
					to := Null
					if apiMerged != nil {
						to = fmt.Sprintf("%v", *apiMerged)
					}
					Printf("Updating PR '%v' merged %s -> %s\n", ic, from, to)
				}
			}

			// Check closed_at change
			changedClosed := false
			if (apiClosedAt == nil && ghaClosedAt != nil) || (apiClosedAt != nil && ghaClosedAt == nil) || (apiClosedAt != nil && ghaClosedAt != nil && ToYMDHMSDate(*apiClosedAt) != ToYMDHMSDate(*ghaClosedAt)) {
				changedClosed = true
				if ctx.Debug > 0 {
					from := Null
					if ghaClosedAt != nil {
						from = fmt.Sprintf("%v", ToYMDHMSDate(*ghaClosedAt))
					}
					to := Null
					if apiClosedAt != nil {
						to = fmt.Sprintf("%v", ToYMDHMSDate(*apiClosedAt))
					}
					Printf("Updating PR '%v' closed_at %s -> %s\n", ic, from, to)
				}
			}

			// Check merged_at change
			changedMergedAt := false
			if (apiMergedAt == nil && ghaMergedAt != nil) || (apiMergedAt != nil && ghaMergedAt == nil) || (apiMergedAt != nil && ghaMergedAt != nil && ToYMDHMSDate(*apiMergedAt) != ToYMDHMSDate(*ghaMergedAt)) {
				changedMergedAt = true
				if ctx.Debug > 0 {
					from := Null
					if ghaMergedAt != nil {
						from = fmt.Sprintf("%v", ToYMDHMSDate(*ghaMergedAt))
					}
					to := Null
					if apiMergedAt != nil {
						to = fmt.Sprintf("%v", ToYMDHMSDate(*apiMergedAt))
					}
					Printf("Updating PR '%v' merged_at %s -> %s\n", ic, from, to)
				}
			}

			// Check milestone change
			changedMilestone := false
			if (apiMilestoneID == nil && ghaMilestoneID != nil) || (apiMilestoneID != nil && ghaMilestoneID == nil) || (apiMilestoneID != nil && ghaMilestoneID != nil && *apiMilestoneID != *ghaMilestoneID) {
				changedMilestone = true
				if ctx.Debug > 0 {
					from := Null
					if ghaMilestoneID != nil {
						from = fmt.Sprintf("%d", *ghaMilestoneID)
					}
					to := Null
					if apiMilestoneID != nil {
						to = fmt.Sprintf("%d", *apiMilestoneID)
					}
					Printf("Updating PR '%v' milestone %s -> %s\n", ic, from, to)
				}
			}

			// Check assignee change
			changedAssignee := false
			if (apiAssigneeID == nil && ghaAssigneeID != nil) || (apiAssigneeID != nil && ghaAssigneeID == nil) || (apiAssigneeID != nil && ghaAssigneeID != nil && *apiAssigneeID != *ghaAssigneeID) {
				changedAssignee = true
				if ctx.Debug > 0 {
					from := Null
					if ghaAssigneeID != nil {
						from = fmt.Sprintf("%d", *ghaAssigneeID)
					}
					to := Null
					if apiAssigneeID != nil {
						to = fmt.Sprintf("%d", *apiAssigneeID)
					}
					Printf("Updating PR '%v' assignee %s -> %s\n", ic, from, to)
				}
			}

			// Check merged by change
			changedMergedBy := false
			if (apiMergedByID == nil && ghaMergedByID != nil) || (apiMergedByID != nil && ghaMergedByID == nil) || (apiMergedByID != nil && ghaMergedByID != nil && *apiMergedByID != *ghaMergedByID) {
				changedMergedBy = true
				if ctx.Debug > 0 {
					from := Null
					if ghaMergedByID != nil {
						from = fmt.Sprintf("%d", *ghaMergedByID)
					}
					to := Null
					if apiMergedByID != nil {
						to = fmt.Sprintf("%d", *apiMergedByID)
					}
					Printf("Updating PR '%v' merged by %s -> %s\n", ic, from, to)
				}
			}

			// Process current labels (they are on the issue not PR, but if changed we should add entry)
			rowsL := QuerySQLWithErr(
				c,
				ctx,
				fmt.Sprintf(
					"select coalesce(string_agg(sub.label_id::text, ','), '') from "+
						"(select label_id from gha_issues_labels where event_id = %s "+
						"order by label_id) sub",
					NValue(1),
				),
				ghaEventID,
			)
			defer func() { FatalOnError(rowsL.Close()) }()
			ghaLabels := ""
			for rowsL.Next() {
				FatalOnError(rowsL.Scan(&ghaLabels))
			}
			FatalOnError(rowsL.Err())
			changedLabels := false
			if ghaLabels != ic.Labels {
				if ctx.Debug > 0 {
					Printf("Updating PR '%v' labels to '%s', they were: '%s' (event_id %d)\n", ic, ic.Labels, ghaLabels, ghaEventID)
				}
				changedLabels = true
			}

			// API Assignees
			AssigneesMap := make(map[int64]string)
			for _, assignee := range pr.Assignees {
				AssigneesMap[*assignee.ID] = *assignee.Login
			}
			assigneesAry := Int64Ary{}
			for assignee := range AssigneesMap {
				assigneesAry = append(assigneesAry, assignee)
			}
			sort.Sort(assigneesAry)
			l = len(assigneesAry)
			apiAssignees := ""
			for i, assignee := range assigneesAry {
				if i == l-1 {
					apiAssignees += fmt.Sprintf("%d", assignee)
				} else {
					apiAssignees += fmt.Sprintf("%d,", assignee)
				}
			}
			// GHA assignees
			rowsA := QuerySQLWithErr(
				c,
				ctx,
				fmt.Sprintf(
					"select coalesce(string_agg(sub.assignee_id::text, ','), '') from "+
						"(select assignee_id from gha_pull_requests_assignees where event_id = %s "+
						"order by assignee_id) sub",
					NValue(1),
				),
				ghaEventID,
			)
			defer func() { FatalOnError(rowsA.Close()) }()
			ghaAssignees := ""
			for rowsA.Next() {
				FatalOnError(rowsA.Scan(&ghaAssignees))
			}
			FatalOnError(rowsA.Err())
			changedAssignees := false
			if ghaAssignees != apiAssignees {
				if ctx.Debug > 0 {
					Printf("Updating PR '%v' assignees to '%s', they were: '%s' (event_id %d)\n", ic, apiAssignees, ghaAssignees, ghaEventID)
				}
				changedAssignees = true
			}

			// API Requested reviewers
			RequestedReviewersMap := make(map[int64]string)
			for _, reviewer := range pr.RequestedReviewers {
				RequestedReviewersMap[*reviewer.ID] = *reviewer.Login
			}
			reviewersAry := Int64Ary{}
			for reviewer := range RequestedReviewersMap {
				reviewersAry = append(reviewersAry, reviewer)
			}
			sort.Sort(reviewersAry)
			l = len(reviewersAry)
			apiRequestedReviewers := ""
			for i, reviewer := range reviewersAry {
				if i == l-1 {
					apiRequestedReviewers += fmt.Sprintf("%d", reviewer)
				} else {
					apiRequestedReviewers += fmt.Sprintf("%d,", reviewer)
				}
			}
			// GHA reviewers
			rowsRV := QuerySQLWithErr(
				c,
				ctx,
				fmt.Sprintf(
					"select coalesce(string_agg(sub.requested_reviewer_id::text, ','), '') from "+
						"(select requested_reviewer_id from gha_pull_requests_requested_reviewers where event_id = %s "+
						"order by requested_reviewer_id) sub",
					NValue(1),
				),
				ghaEventID,
			)
			defer func() { FatalOnError(rowsRV.Close()) }()
			ghaRequestedReviewers := ""
			for rowsRV.Next() {
				FatalOnError(rowsRV.Scan(&ghaRequestedReviewers))
			}
			FatalOnError(rowsRV.Err())
			changedRequestedReviewers := false
			if ghaRequestedReviewers != apiRequestedReviewers {
				if ctx.Debug > 0 {
					Printf("Updating PR '%v' requested reviewers to '%s', they were: '%s' (event_id %d)\n", ic, apiRequestedReviewers, ghaRequestedReviewers, ghaEventID)
				}
				changedRequestedReviewers = true
			}

			uidx := 2
			// Do the update if needed
			changedAnything := changedMilestone || changedState || changedClosed || changedMerged || changedMergedAt || changedMergedBy || changedAssignee || changedTitle || changedLabels || changedAssignees || changedRequestedReviewers
			if changedAnything {
				uidx = 3
				FatalOnError(
					ArtificialPREvent(
						c,
						ctx,
						&ic,
						&pr,
					),
				)
			}

			if ctx.Debug > 0 {
				if manual {
					Printf("PR Event exist (event_id: %d), added artificial: %v: '%v'\n", ghaEventID, changedAnything, ic)
				} else {
					Printf("PR Event for the same date (%v) exist (event_id: %d), added artificial: %v: '%v'\n", updatedAt, ghaEventID, changedAnything, ic)
				}
			}
			updatesMutex.Lock()
			updates[uidx]++
			updatesMutex.Unlock()
			// Synchronize go routine
			ch <- changedAnything
		}(ch, iid)

		// go routine called with 'ch' channel to sync and tag index
		nThreads++
		if nThreads == thrN {
			<-ch
			nThreads--
			checked++
			ProgressInfo(checked, nIssues, dtStart, &lastTime, time.Duration(10)*time.Second, "")
		}
	}
	// Usually all work happens on '<-ch'
	for nThreads > 0 {
		<-ch
		nThreads--
		checked++
		ProgressInfo(checked, nIssues, dtStart, &lastTime, time.Duration(10)*time.Second, "")
	}
	// Get RateLimits info
	_, rem, wait = GetRateLimits(gctx, gc, true)
	if manual {
		Printf(
			"ghapi2db.go: Manually processed %d PRs (%d new PRs, existing: %d not needed, %d added): %d API points remain, resets in %v\n",
			checked, updates[0], updates[2], updates[3], rem, wait,
		)
	} else {
		Printf(
			"ghapi2db.go: Automatically processed %d PRs (%d new for date, %d artificial exists, date exists: %d not needed, %d added, collisions: %d): %d API points remain, resets in %v\n",
			checked, updates[0], updates[1], updates[2], updates[3], updates[4], rem, wait,
		)
	}
}
