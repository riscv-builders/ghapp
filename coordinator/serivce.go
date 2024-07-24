package coordinator

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/riscv-builders/service/db"
	"github.com/riscv-builders/service/models"
	"github.com/uptrace/bun"
)

type Service struct {
	cfg *Config
	db  *bun.DB

	apiTimeout    time.Duration
	tokenDuration time.Duration

	jobCh chan *models.GithubWorkflowJobEvent
}

func New(cfg *Config) (*Service, error) {
	s := &Service{
		cfg: cfg,
	}
	var err error
	s.db, err = db.New(cfg.DBURL, cfg.DBType)
	if err != nil {
		return nil, err
	}
	s.apiTimeout = 10 * time.Second
	s.tokenDuration = 5 * 24 * time.Hour
	s.Migrate()
	return s, nil
}

func (s *Service) Migrate() (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err = s.db.NewCreateTable().Model((*models.Builder)(nil)).Exec(ctx)
	_, err = s.db.NewCreateTable().Model((*models.Session)(nil)).Exec(ctx)
	_, err = s.db.NewInsert().Model(&models.Builder{
		Name:        "root",
		AccessToken: "root",
		Sponsor:     "mengzhuo",
		Status:      "missing",
		Labels:      map[string]string{"cpu": "2", "region": "cn"},
	}).Exec(ctx)
	return
}

func (s *Service) Serve() error {
	r := gin.Default()
	r.POST("/login", s.Login)
	r.PUT("/self/session", s.RefreshSession)
	r.POST("/job", s.SearchJob)
	// r.PUT("/job/:id/status", s.UpdataJobStatus)
	// r.PUT("/job/:id/logs", s.UpdataJobLogs)

	return r.Run(s.cfg.ListenAddr)
}

type LoginRequest struct {
	Name        string            `form:"name" json:"name" binding:"required"`
	AccessToken string            `form:"access_token" json:"access_token" binding:"required"`
	Labels      map[string]string `form:"labels" json:"labels"`
}

func (s *Service) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.apiTimeout)
	defer cancel()

	bdr := &models.Builder{Name: req.Name, AccessToken: req.AccessToken}
	err := s.db.NewSelect().Model(bdr).Where("name = ? AND access_token = ?",
		req.Name, req.AccessToken).Scan(ctx)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if bdr.ID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	session := &models.Session{}
	count, err := s.db.NewSelect().Model(session).Where("builder_id = ?", bdr.ID).Count(ctx)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	token := uuid.New().String()
	expiredAt := time.Now().Add(s.tokenDuration)
	labels := session.Labels
	if labels == nil {
		labels = make(map[string]string)
	}
	// builders labels override!
	for k, v := range bdr.Labels {
		labels[k] = v
	}
	if count != 0 {
		session.Token = token
		session.ExpiredAt = expiredAt
		session.Labels = labels
		_, err = tx.NewUpdate().Model(session).
			Column("token", "expired_at", "labels", "updated_at").
			Where("builder_id = ?", bdr.ID).Exec(ctx)
	} else {
		session = &models.Session{
			Builder:   *bdr,
			BuilderID: bdr.ID,
			Token:     token,
			ExpiredAt: expiredAt,
			Labels:    labels,
		}
		_, err = tx.NewInsert().Model(session).Exec(ctx)
	}
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		tx.Rollback()
		return
	}

	bdr.LastSeen = bun.NullTime{time.Now()}
	_, err = tx.NewUpdate().Model(bdr).WherePK().Column("last_seen").Exec(ctx)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		tx.Rollback()
		return
	}

	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"session": session})
}

func (s *Service) RefreshSession(c *gin.Context) {
	token := c.GetHeader("X-RISCV-Builders-Token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.apiTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	session := &models.Session{
		Token:     token,
		ExpiredAt: time.Now().Add(s.tokenDuration),
	}
	hit, err := tx.NewUpdate().Model(session).
		Column("expired_at", "updated_at").
		Where("token = ?", token).Exec(ctx)

	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		tx.Rollback()
		return
	}

	if rows, _ := hit.RowsAffected(); rows == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token"})
		tx.Rollback()
		return
	}

	tx.Commit()
	c.JSON(http.StatusOK, session)
}

func (s *Service) SearchJob(c *gin.Context) {
	token := c.GetHeader("X-RISCV-Builders-Token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.apiTimeout)
	defer cancel()

	session := &models.Session{}
	err := s.db.NewSelect().Model(session).Where("token = ?", token).
		Join("JOIN builders AS b ON b.id = session.builder_id").
		Limit(1).Scan(ctx)

	if err != nil {
		if strings.Contains(err.Error(), "no rows in result set") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token"})
			return
		}
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	if session.ExpiredAt.Before(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token"})
		return
	}
	c.JSON(200, gin.H{"job": <-s.jobCh})
}
