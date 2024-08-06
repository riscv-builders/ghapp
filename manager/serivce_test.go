package manager

import (
	"context"
	"testing"

	"github.com/google/go-github/v62/github"
)

func TestJWT(t *testing.T) {
	c := &Coor{cfg: &Config{
		ClientID: "Iv23liq6HZyPLflIO753",
	}}

	var err error
	c.privateKey, err = loadPrivateFile("../gh_private.pem")
	if err != nil {
		t.Fatal(err)
	}

	jwt, err := c.getJWT()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(jwt)

	cli := github.NewClient(nil).WithAuthToken(jwt)
	self, resp, err := cli.Apps.Get(context.Background(), "")
	if err != nil {
		t.Error(err)
	}
	t.Log(self.GetID(), resp)

	install, _, err := cli.Apps.CreateInstallationToken(context.Background(), 52030363, nil)
	t.Log(install.GetToken(), install.GetExpiresAt(), resp)

	repocli := github.NewClient(nil).WithAuthToken(install.GetToken())

	token, _, err := repocli.Actions.CreateRegistrationToken(context.Background(), "riscv-builders", "mock-repo")
	t.Log(token.GetToken(), err)
}
