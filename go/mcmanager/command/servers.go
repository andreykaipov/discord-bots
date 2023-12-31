package command

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type serverConfig struct {
	CheckTimeout          time.Duration `yaml:"check_timeout"`
	CheckInterval         time.Duration `yaml:"check_interval"`
	DeallocationThreshold int           `yaml:"deallocation_threshold"`
	Servers               []*server     `yaml:"servers"`
}

type server struct {
	Host                  string        `yaml:"host"`
	Name                  string        `yaml:"name"`
	ResourceGroup         string        `yaml:"resource_group"`
	CheckTimeout          time.Duration `yaml:"check_timeout"`
	CheckInterval         time.Duration `yaml:"check_interval"`
	DeallocationThreshold int           `yaml:"deallocation_threshold"`

	host        string
	port        string
	checkCount  int
	checkErrors int
	online      bool
}

func (c *Discord) findServerFuzzy(host string) (*server, error) {
	servers := []*server{}
	for _, server := range c.serverConfig.Servers {
		if strings.Contains(server.Host, host) {
			servers = append(servers, server)
		}
	}
	switch len(servers) {
	case 0:
		return nil, fmt.Errorf("server not found")
	case 1:
		return servers[0], nil
	default:
		return nil, fmt.Errorf("multiple servers found for %q, please be more specific", host)
	}
}

func (c *Discord) setConfigDefaults() error {
	cfg := c.serverConfig
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = 5 * time.Minute
	}
	if cfg.DeallocationThreshold == 0 {
		cfg.DeallocationThreshold = 5
	}
	if cfg.CheckTimeout == 0 {
		cfg.CheckTimeout = 5 * time.Second
	}
	for _, s := range cfg.Servers {
		if err := c.setServerDefaults(s); err != nil {
			return err
		}
	}
	return nil
}

func (c *Discord) setServerDefaults(s *server) error {
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
	if s.CheckTimeout == 0 {
		s.CheckTimeout = c.serverConfig.CheckTimeout
	}
	if s.CheckInterval == 0 {
		s.CheckInterval = c.serverConfig.CheckInterval
	}
	if s.DeallocationThreshold == 0 {
		s.DeallocationThreshold = c.serverConfig.DeallocationThreshold
	}
	return nil
}

func (c *Discord) checkServer(s *server) (*Pong, error) {
	ping := &Ping{}
	pong, err := ping.Check(s.host, s.port, s.CheckTimeout)
	if err != nil {
		return nil, err
	}
	return pong, nil
}

func (c *Discord) startServer(s *server) (string, error) {
	ping := &Ping{}
	pong, err := ping.Check(s.host, s.port, s.CheckTimeout)
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
			s.setStatus("online")
			return fmt.Sprintf("%s is already starting, please wait you impatient animal", s.Host), nil
		case "PowerState/running":
			s.setStatus("online")
			return fmt.Sprintf("%s is running, but Minecraft isn't up yet", s.Host), nil
		default:
		}
	}

	s.setStatus("online")
	poller, err := c.vmClient.BeginStart(context.Background(), s.ResourceGroup, s.Name, nil)
	if err != nil {
		return "", fmt.Errorf("starting server: %s", err)
	}
	_, err = poller.PollUntilDone(context.Background(), nil)
	if err != nil {
		return "", fmt.Errorf("polling until start complete: %s", err)
	}

	return fmt.Sprintf("%s started", s.Host), nil
}

func (c *Discord) deallocateServer(s *server) (string, error) {
	ping := &Ping{}
	pong, err := ping.Check(s.host, s.port, s.CheckTimeout)
	if err == nil && pong.PlayerCount > 0 {
		return fmt.Sprintf("%s has %d players; that would be rude", s.Host, pong.PlayerCount), nil
	}

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
			s.setStatus("offline")
			return fmt.Sprintf("%s is already deallocating", s.Host), nil
		case "PowerState/deallocated":
			s.setStatus("offline")
			return fmt.Sprintf("%s is already deallocated", s.Host), nil
		default:
		}
	}

	s.setStatus("offline")
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

func (s *server) setStatus(status string) {
	switch status {
	case "online":
		s.online = true
	case "offline":
		s.online = false
	default:
		panic(fmt.Sprintf("invalid status: %s", status))
	}
	s.checkCount = 0
	s.checkErrors = 0
}
