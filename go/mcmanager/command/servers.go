package command

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type serverConfig struct {
	CheckInterval         time.Duration `yaml:"check_interval"`
	DeallocationThreshold int           `yaml:"deallocation_threshold"`
	Servers               []*server     `yaml:"servers"`
}

type server struct {
	Host          string        `yaml:"host"`
	Name          string        `yaml:"name"`
	ResourceGroup string        `yaml:"resource_group"`
	Timeout       time.Duration `yaml:"timeout"`

	host       string
	port       string
	checkCount int
	online     bool
}

func (c *Discord) findServerFuzzy(host string) (*server, error) {
	for _, server := range c.serverConfig.Servers {
		if strings.Contains(server.Host, host) {
			return server, nil
		}
	}
	return nil, fmt.Errorf("server not found")
}

func (cfg *serverConfig) setDefaults() error {
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = 5 * time.Minute
	}
	if cfg.DeallocationThreshold == 0 {
		cfg.DeallocationThreshold = 5
	}
	for _, s := range cfg.Servers {
		if err := s.setDefaults(); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) setDefaults() error {
	parts := strings.Split(s.Host, ":")
	switch len(parts) {
	case 1:
		s.host = parts[0]
		s.port = "19132"
	case 2:
		s.host = parts[0]
		s.port = parts[1]
	default:
		return fmt.Errorf("invalid server host: %s", s.Host)
	}
	if s.Name == "" {
		s.Name = s.host
	}
	if s.ResourceGroup == "" {
		s.ResourceGroup = fmt.Sprintf("%s-rg", s.Name)
	}
	if s.Timeout == 0 {
		s.Timeout = 5 * time.Second
	}
	return nil
}

func (c *Discord) checkServer(s *server) (*Pong, error) {
	ping := &Ping{}
	pong, err := ping.Check(s.host, s.port, s.Timeout)
	if err != nil {
		return nil, err
	}
	return pong, nil
}

func (c *Discord) startServer(s *server) (string, error) {
	// this might be an issue if multiple start requests are issued. in this
	// case, start server will return early (e.g. because the server is
	// already starting), at which point this defer func will be called much
	// earlier, marking the server online before it actually is. however,
	// it's not big of a deal...
	defer func() {
		s.online = true
		s.checkCount = 0
	}()

	ping := &Ping{}
	pong, err := ping.Check(s.host, s.port, s.Timeout)
	if err == nil {
		return fmt.Sprintf("%s is already running with %d players", s.Host, pong.PlayerCount), nil
	}

	// check if vm is already running
	resp, err := c.vmClient.InstanceView(context.Background(), s.ResourceGroup, s.Name, nil)
	if err != nil {
		return "", err
	}
	for _, status := range resp.Statuses {
		switch *status.Code {
		case "ProvisioningState/updating":
			return fmt.Sprintf("%s is currently updating, wait for it to finish whatever it's doing", s.Host), nil
		case "PowerState/starting":
			return fmt.Sprintf("%s is already starting, please wait you impatient animal", s.Host), nil
		case "PowerState/running":
			return fmt.Sprintf("%s is running, but Minecraft isn't up yet", s.Host), nil
		default:
		}
	}
	poller, err := c.vmClient.BeginStart(context.Background(), s.ResourceGroup, s.Name, nil)
	if err != nil {
		return "", fmt.Errorf("starting server: %s", err)
	}
	_, err = poller.PollUntilDone(context.Background(), nil)
	if err != nil {
		return "", fmt.Errorf("polling until start complete: %s", err)
	}

	/*
		attempts := 0
		for {
			if _, err := c.checkServer(s); err == nil {
				break
			}
			if attempts >= 5 {
				return "", fmt.Errorf("waiting for minecraft to start: %s", err)
			}
			// shouldn't take longer than this for the mc process to start
			time.Sleep(30 * time.Second)
			attempts++
		}
	*/

	return fmt.Sprintf("%s started", s.Host), nil
}

func (c *Discord) deallocateServer(s *server) (string, error) {
	defer func() {
		s.online = false
		s.checkCount = 0
	}()

	// check if vm is already stopped
	resp, err := c.vmClient.InstanceView(context.Background(), s.ResourceGroup, s.Name, nil)
	if err != nil {
		return "", err
	}
	for _, status := range resp.Statuses {
		switch *status.Code {
		case "ProvisioningState/updating":
			return fmt.Sprintf("%s is currently updating, wait for it to finish whatever it's doing", s.Host), nil
		case "PowerState/deallocating":
			return fmt.Sprintf("%s is already deallocating", s.Host), nil
		case "PowerState/deallocated":
			return fmt.Sprintf("%s is already deallocated", s.Host), nil
		default:
		}
	}
	poller, err := c.vmClient.BeginDeallocate(context.Background(), s.ResourceGroup, s.Name, nil)
	if err != nil {
		return "", fmt.Errorf("deallocating server: %s", err)
	}
	_, err = poller.PollUntilDone(context.Background(), nil)
	if err != nil {
		return "", fmt.Errorf("polling until deallocation complete: %s", err)
	}

	return fmt.Sprintf("%s deallocated", s.Host), nil
}
