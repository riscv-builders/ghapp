package webhook

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/go-github/v62/github"
	"github.com/riscv-builders/ghapp/models"
)

func (c *GithubService) GithubEvents(w http.ResponseWriter, r *http.Request) {
	slog.Debug("webhook", "method", r.Method, "url", r.URL)

	payload, err := github.ValidatePayload(r, []byte(c.cfg.GHSecretKey))
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	event, err := github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	slog.Debug("hook inbound", "type", fmt.Sprintf("%T", event))
	switch event := event.(type) {
	case *github.PingEvent:
		slog.Info(event.GetZen())
	case *github.WorkflowJobEvent:
		err = c.handleWorkflowJobEvent(event)
	case *github.InstallationTargetEvent:
		err = c.handleInstallTarget(event)
	case *github.InstallationEvent:
		err = c.handleInstall(event)
	default:
		slog.Info("Unknow type, skipped")
		slog.Debug(fmt.Sprintf("%v", event))
		fmt.Fprintf(w, "Unknow type, skipped")
	}
	if err != nil {
		slog.Error("handle hook error", "msg", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (c *GithubService) handleInstallTarget(event *github.InstallationTargetEvent) (err error) {
	slog.Debug("install app target", "action", event.GetAction(),
		"target_type", event.GetTargetType())

	switch event.GetAction() {
	case "added":
	case "removed":
	default:
		slog.Warn("invalid install target action", "action", event.GetAction())
	}
	return
}

func (c *GithubService) handleInstall(event *github.InstallationEvent) (err error) {
	install := event.GetInstallation()
	slog.Debug("install app", "action", event.GetAction(),
		"target_type", install.GetTargetType())
	switch event.GetAction() {
	case "created":
	case "removed":
	default:
		slog.Warn("invalid install action", "action", event.GetAction())
	}
	return
}

func (c *GithubService) handleWorkflowJobEvent(event *github.WorkflowJobEvent) (err error) {
	wf := event.GetWorkflowJob()
	if wf == nil {
		return fmt.Errorf("invalid workflow job")
	}

	repo := event.GetRepo()
	if repo == nil {
		return fmt.Errorf("invalid repo")
	}

	if !models.HasRVBLabels(wf.Labels) {
		slog.Warn("skip event", "run_id", wf.GetRunID(),
			"labels", wf.Labels,
			"owner", repo.Owner.GetLogin(),
			"repo", repo.GetName(),
			"reason", "labels not supported")
		return
	}

	action := models.GithubWorkflowJobStatus(event.GetAction())
	switch action {
	case models.WorkflowJobQueued,
		models.WorkflowJobCompleted,
		models.WorkflowJobInProgress:
	default:
		slog.Warn("skip event", "run_id", wf.GetRunID(),
			"action", action, "reason", "action not supported")
		return
	}

	var installID int64
	if install := event.GetInstallation(); install != nil && install.GetID() != 0 {
		installID = install.GetID()
	}

	if installID == 0 {
		slog.Warn("skip event", "id", event.GetWorkflowJob().GetID(), "reason", "install id is empty")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.ghtimeout)
	defer cancel()

	slog.Info("WorkflowJob", "action", action)
	slog.Debug(fmt.Sprintf("%v", event))

	id := wf.GetID()
	repoID := repo.GetID()
	runID := wf.GetRunID()

	oldJob := &models.GithubWorkflowJob{}
	mod := &models.GithubWorkflowJob{
		ID:             wf.GetID(),
		RunID:          wf.GetRunID(),
		Name:           wf.GetName(),
		Owner:          repo.Owner.GetLogin(),
		RepoName:       repo.GetName(),
		RepoID:         repo.GetID(),
		Status:         action,
		HeadSHA:        wf.GetHeadSHA(),
		Labels:         wf.Labels,
		RunnerName:     wf.GetRunnerName(),
		RunnerID:       wf.GetRunnerID(),
		InstallationID: installID,
		WorkflowName:   wf.GetWorkflowName(),
	}

	err = c.db.NewSelect().Model((*models.GithubWorkflowJob)(nil)).
		Where("id = ? AND run_id = ? AND repo_id = ?", id, runID, repoID).Scan(ctx, oldJob)

	switch err {
	case sql.ErrNoRows:
		if mod.Status != models.WorkflowJobQueued {
			slog.Warn("new wf job is not queued", "id", mod.ID)
		}
		_, ierr := c.db.NewInsert().Model(mod).Ignore().Exec(ctx)
		return ierr
	case nil:
		slog.Info("workflow", "id", id, "run_id", runID,
			"old_status", oldJob.Status, "new_status", mod.Status,
			"changed", oldJob.IsStatusChangable(mod.Status))
		if oldJob.IsStatusChangable(mod.Status) {
			_, err = c.db.NewUpdate().Model(mod).Column("status", "updated_at").
				Where("id = ? AND run_id = ? AND repo_id = ?", id, runID, repoID).Exec(ctx)
		}
	}
	return err
}
