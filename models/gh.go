package models

import (
	"context"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

type GithubWorkflowJob struct {
	ID      int64                   `bun:"id,pk" json:"id"`
	RunID   int64                   `bun:",notnull" json:"run_id"`
	HeadSHA string                  `bun:",notnull" json:"head_sha"`
	Name    string                  `json:"name"`
	Status  GithubWorkflowJobStatus `json:"status"`

	// Runner related
	Labels       []string `json:"labels"`
	RunnerID     int64    `json:"runner_id"`
	RunnerName   string   `json:"runner_name"`
	WorkflowName string   `json:"workflow_name"`

	// Repo
	Owner    string `json:"owner"`
	RepoID   int64  `bun:",notnull" json:"repo_id"`
	RepoName string `json:"repo_name"`

	// Install
	InstallationID int64 `json:"installation_id"`

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

type GithubWorkflowJobStatus string

const (
	WorkflowJobQueued     GithubWorkflowJobStatus = "queued"
	WorkflowJobScheduled                          = "scheduled"
	WorkflowJobInProgress                         = "in_progress"
	WorkflowJobCompleted                          = "completed"
	WorkflowJobTimeout                            = "timeout"
	WorkflowJobFailed                             = "failed"
)

func (g *GithubWorkflowJob) IsStatusChangable(ns GithubWorkflowJobStatus) bool {
	if g.Status == ns {
		return false // same, ignore
	}

	switch g.Status {
	case "":
		return true
	case WorkflowJobQueued:
		return ns == WorkflowJobScheduled || ns == WorkflowJobInProgress || ns == WorkflowJobCompleted
	case WorkflowJobScheduled:
		return ns == WorkflowJobInProgress || ns == WorkflowJobCompleted
	case WorkflowJobInProgress:
		return ns == WorkflowJobCompleted
	}
	return false
}

func HasRVBLabels(sl []string) bool {
	for _, s := range sl {
		sub := strings.Split(s, ",")
		for _, ss := range sub {
			if strings.EqualFold(ss, "riscv-builders") {
				return true
			}
		}
	}
	return false
}

func GetRVBLabels(sl []string) []string {
	return nil
}

var _ bun.AfterCreateTableHook = (*GithubWorkflowJob)(nil)

func (*GithubWorkflowJob) AfterCreateTable(ctx context.Context, query *bun.CreateTableQuery) error {
	_, err := query.DB().NewCreateIndex().
		Model((*GithubWorkflowJob)(nil)).
		Index("gh_job_run_repo_idx").
		Column("id").
		Column("run_id").
		Column("repo_id").
		Unique().Exec(ctx)
	if err != nil {
		return err
	}

	_, err = query.DB().NewCreateIndex().
		Model((*GithubWorkflowJob)(nil)).
		Index("gh_job_status_idx").
		Column("status").Exec(ctx)
	return err
}

var _ bun.BeforeAppendModelHook = (*GithubWorkflowJob)(nil)

func (g *GithubWorkflowJob) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	switch query.(type) {
	case *bun.InsertQuery:
		g.CreatedAt = time.Now()
		g.UpdatedAt = time.Now()
	case *bun.UpdateQuery:
		g.UpdatedAt = time.Now()
	}
	return nil
}

type GithubInstall struct {
}
