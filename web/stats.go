package web

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/riscv-builders/ghapp/models"
)

type Stats struct {
	JobCount     int       `json:"job_count"`
	RepoCount    int       `json:"repo_count"`
	IdleBuilder  int       `json:"idle_builder"`
	TotalBuilder int       `json:"total_builder"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (c *GithubService) handleStats(g *gin.Context) {
	if c.stats.UpdatedAt.Add(time.Minute).After(time.Now()) {
		g.JSON(http.StatusOK, c.stats)
		return
	}

	ctx := g.Request.Context()
	s := &Stats{UpdatedAt: time.Now()}
	var err error
	s.RepoCount, err = c.db.NewSelect().Model((*models.GithubWorkflowJob)(nil)).
		DistinctOn("github_workflow_job.repo_id").Count(ctx)
	if err != nil {
		g.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	s.IdleBuilder, err = c.db.NewSelect().Model((*models.Builder)(nil)).
		Where("status = ?", models.BuilderIdle).Count(ctx)
	if err != nil {
		g.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	s.TotalBuilder, err = c.db.NewSelect().Model((*models.Builder)(nil)).Count(ctx)
	if err != nil {
		g.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	s.JobCount, err = c.db.NewSelect().Model((*models.GithubWorkflowJob)(nil)).Count(ctx)
	if err != nil {
		g.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.stats = *s
	g.JSON(http.StatusOK, s)
	return
}
