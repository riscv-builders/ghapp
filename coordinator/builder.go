package coordinator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/riscv-builders/service/models"
	"golang.org/x/crypto/ssh"
)

func (c *Coor) runJob(ctx context.Context, job *models.GithubWorkflowJob, bdr *models.Builder) error {
	if bdr.Status != models.BuilderLocked {
		return fmt.Errorf("builder:%d not setup", bdr.ID)
	}
	if job.Status != models.WorkflowJobQueued {
		return fmt.Errorf("job:%d not queued", job.ID)
	}

	tx, err := c.db.Begin()
	if err != nil {
		return err
	}

	bdr.Status = models.BuilderWorking
	tx.NewUpdate().Model(bdr).Column("status").WherePK().Exec(ctx)

	job.Status = models.WorkflowJobScheduled
	tx.NewUpdate().Model(job).Column("status").WherePK().Exec(ctx)

	err = tx.Commit()
	if err != nil {
		return err
	}

	switch bdr.Type {
	case models.BuilderSSH:
		return c.runSSHBuilder(ctx, job, bdr)
	}
	return fmt.Errorf("unsupported builder:%s", bdr.Type)
}

func (c *Coor) runSSHBuilder(ctx context.Context, job *models.GithubWorkflowJob, bdr *models.Builder) error {

	token, _, err := c.getActionRegistrationToken(ctx, job.InstallationID, job.Owner, job.RepoName)
	if err != nil {
		return errors.Join(err, fmt.Errorf("registration token:%s", token))
	}

	runner := &models.Runner{
		BuilderID:      bdr.ID,
		Builder:        bdr,
		Job:            job,
		JobID:          job.ID,
		RegToken:       token,
		Name:           fmt.Sprintf("riscv-builder-%s", bdr.Name),
		Labels:         append([]string{"riscv-builers"}, bdr.Labels...),
		SystemLabels:   []string{"riscv64", "riscv", "linux"},
		URL:            fmt.Sprintf("https://github.com/%s/%s", job.Owner, job.RepoName),
		Ephemeral:      true,
		Status:         models.RunnerScheduled,
		TokenExpiredAt: time.Now().Add(5 * 24 * time.Hour),
	}
	_, err = c.db.NewInsert().Model(runner).Exec(ctx)
	return err
}

func (c *Coor) getSSHClient(ctx context.Context, bdr *models.Builder) (*ssh.Client, error) {
	priv := bdr.Token // private key for this builder
	addr := bdr.Meta["addr"]
	user := bdr.Meta["user"]
	// check act runner available
	if priv == "" || user == "" || addr == "" {
		return nil, fmt.Errorf("invalid ssh builder:%s user:%s", addr, user)
	}

	signer, err := ssh.ParsePrivateKey([]byte(priv))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("can't parse private key for builder:%s", bdr.Name), err)
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO check this
	}

	return ssh.Dial("tcp", addr, config)
}

func (c *Coor) prepareSSHBuilder(ctx context.Context, bdr *models.Builder) error {
	client, err := c.getSSHClient(ctx, bdr)
	if err != nil {
		return err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return err
	}

	defer session.Close()
	out, err := session.Output("github-act-runner --version")
	if err != nil {
		bdr.Status = models.BuilderQuarantined
		c.db.NewUpdate().Model(bdr).Column("status").WherePK().Exec(ctx)
		return errors.Join(err, errors.New(string(out)))
	}
	bdr.Meta["runner-version"] = string(out)
	_, err = c.db.NewUpdate().Model(bdr).Column("meta").WherePK().Exec(ctx)
	return err
}
