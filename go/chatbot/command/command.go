package command

import (
	"fmt"
	"math/rand"
	"os/exec"
	"time"

	"github.com/alecthomas/kong"
)

type Context struct {
	Kong      *kong.Context `kong:"-"`
	startTime time.Time
}

type CommonFlags struct{}

type Command struct {
	Context `kong:"-"`
	CommonFlags
}

func (ctx *Context) BeforeResolve(ctxKong *kong.Context) error {
	ctx.Kong = ctxKong
	ctx.Kong.Bind(ctx)
	return nil
}

func (cmd *Command) BeforeResolve(ctx *Context) error {
	cmd.Context = *ctx
	cmd.Context.Kong.Bind(cmd)
	return nil
}

func (ctx *Context) BeforeApply() error {
	ctx.startTime = time.Now()
	rand.Seed(ctx.startTime.UnixNano())
	return nil
}

func (ctx *Context) AfterApply(cmd *Command) error {

	return nil
}

func (cmd *Command) AfterApply() error {
	return nil
}

func (cmd *Command) Run() error {
	return fmt.Errorf("command not implemented")
}

func (ctx *Context) run(script string) (string, error) {
	cmd := exec.Command("/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		err = fmt.Errorf("exec: %s: %w; output:\n%s", script, err, out)
	}
	return string(out), err
}
