package coordinator

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v62/github"
	"github.com/riscv-builders/ghapp/models"
)

var zeroTime = time.Time{}

func (c *Coor) getJWT() (string, error) {

	if c.jwt != "" && c.jwtExpireAt.After(time.Now()) {
		return c.jwt, nil
	}

	c.jwtExpireAt = time.Now().Add(8 * time.Minute)

	claims := &jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)), // Github max exp 10 minute
		Issuer:    c.cfg.ClientID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	var err error
	c.jwt, err = token.SignedString(c.privateKey)
	return c.jwt, err
}

func (c *Coor) createAccessToken(ctx context.Context, installID int64) (*github.InstallationToken, error) {
	jwt, err := c.getJWT()
	if err != nil {
		return nil, err
	}

	cli := github.NewClient(nil).WithAuthToken(jwt)
	install, resp, err := cli.Apps.CreateInstallationToken(ctx, installID, nil)
	slog.Debug("create access token", "err", err, "resp", resp)
	return install, err
}

func (c *Coor) getAccessTokenForInstall(ctx context.Context, installID int64) (string, error) {

	token := &models.Token{Type: models.InstallAccessToken}
	err := c.db.NewSelect().Model((*models.Token)(nil)).
		Where("type = ? AND key = ?", models.InstallAccessToken, installID).
		Limit(1).Scan(ctx, token)

	switch err {
	case nil:
		if token.ExpiredAt.After(time.Now()) {
			return token.Value, nil
		}
		install, err := c.createAccessToken(ctx, installID)
		if err != nil {
			return "", err
		}
		token.Value = install.GetToken()
		token.ExpiredAt = install.GetExpiresAt().Time
		_, err = c.db.NewUpdate().Model(token).Column("value", "expired_at").WherePK().Exec(ctx)
		if err != nil {
			return "", err
		}
		return token.Value, nil
	case sql.ErrNoRows:
		install, err := c.createAccessToken(ctx, installID)
		if err != nil {
			return "", err
		}
		token.Key = strconv.FormatInt(installID, 10)
		token.ExpiredAt = install.GetExpiresAt().Time
		token.Value = install.GetToken()
		_, err = c.db.NewInsert().Model(token).Exec(ctx)
		if err != nil {
			return "", err
		}
		return token.Value, nil
	}
	return "", err
}

func (c *Coor) createActionRegistrationToken(ctx context.Context, installID int64, owner string, repo string) (*github.RegistrationToken, error) {
	at, err := c.getAccessTokenForInstall(ctx, installID)
	if err != nil {
		return nil, err
	}

	repocli := github.NewClient(nil).WithAuthToken(at)
	token, _, err := repocli.Actions.CreateRegistrationToken(ctx, owner, repo)
	return token, err
}

func (c *Coor) getActionRegistrationToken(ctx context.Context, installID int64, owner string, repo string) (string, time.Time, error) {
	if owner == "" || repo == "" || installID == 0 {
		return "", zeroTime, fmt.Errorf("invalid action registration request")
	}
	key := fmt.Sprintf("%d||%s||%s", installID, owner, repo)

	token := &models.Token{Type: models.ActionRegistrationToken, Key: key}
	err := c.db.NewSelect().Model((*models.Token)(nil)).
		Where("type = ? AND key = ?", models.ActionRegistrationToken, key).
		Limit(1).Scan(ctx, token)
	switch err {
	case nil:
		if token.ExpiredAt.After(time.Now()) {
			return token.Value, token.ExpiredAt, nil
		}
		at, err := c.createActionRegistrationToken(ctx, installID, owner, repo)
		if err != nil {
			return "", zeroTime, err
		}
		token.ExpiredAt = at.ExpiresAt.Time
		token.Value = at.GetToken()
		_, err = c.db.NewUpdate().Model(token).Column("value", "expired_at").WherePK().Exec(ctx)
		if err != nil {
			return "", zeroTime, err
		}
		return token.Value, token.ExpiredAt, nil

	case sql.ErrNoRows:
		at, err := c.createActionRegistrationToken(ctx, installID, owner, repo)
		if err != nil {
			return "", zeroTime, err
		}
		token.ExpiredAt = at.GetExpiresAt().Time
		token.Value = at.GetToken()
		_, err = c.db.NewInsert().Model(token).Exec(ctx)
		if err != nil {
			return "", zeroTime, err
		}
		return token.Value, token.ExpiredAt, nil
	}
	return "", zeroTime, err
}
