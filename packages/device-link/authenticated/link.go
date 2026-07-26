package authenticated

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	devicelink "github.com/tutti-os/tutti/packages/device-link"
	"github.com/tutti-os/tutti/packages/device-link/icequic"
)

type Role string

const (
	RoleCaller Role = "caller"
	RoleOwner  Role = "owner"
)

type ParticipantConfig struct {
	STUNEndpoints         []string
	NetworkPolicy         string
	STUNGatherTimeout     time.Duration
	ExcludeHostCandidates bool
	IncludeLoopback       bool
	// CompatibleProtocols appends temporary legacy ALPN values after the
	// canonical DeviceLink protocol. It exists for rolling consumer migrations;
	// new integrations leave it empty.
	CompatibleProtocols []string
}

type Description struct {
	Fingerprint string   `json:"fingerprint"`
	Ufrag       string   `json:"ufrag"`
	Pwd         string   `json:"pwd"`
	Candidates  []string `json:"candidates"`
}

type Participant struct {
	identity  devicelink.Identity
	agent     *icequic.Agent
	protocols []string

	mu             sync.Mutex
	connectStarted bool
	connectCancel  context.CancelFunc
	link           *Link
	closed         bool
}

func NewParticipant(cfg ParticipantConfig) (*Participant, error) {
	identity, err := devicelink.NewEphemeralIdentity(time.Now())
	if err != nil {
		return nil, err
	}
	protocols := append([]string{devicelink.ALPN}, cfg.CompatibleProtocols...)
	if _, err := identity.ClientTLSConfigForProtocols(identity.Fingerprint, protocols); err != nil {
		return nil, fmt.Errorf("configure device-link protocols: %w", err)
	}
	agent, err := icequic.NewAgent(icequic.AgentConfig{
		STUNEndpoints:         append([]string(nil), cfg.STUNEndpoints...),
		NetworkPolicy:         icequic.NetworkPolicy(strings.TrimSpace(cfg.NetworkPolicy)),
		STUNGatherTimeout:     cfg.STUNGatherTimeout,
		ExcludeHostCandidates: cfg.ExcludeHostCandidates,
		IncludeLoopback:       cfg.IncludeLoopback,
	})
	if err != nil {
		return nil, err
	}
	return &Participant{identity: identity, agent: agent, protocols: protocols}, nil
}

// StartLocalDescription starts asynchronous candidate gathering and returns
// the current authenticated description immediately. Candidates may be empty;
// callers publish later snapshots when LocalCandidateChanges fires.
func (p *Participant) StartLocalDescription() (Description, error) {
	agent, fingerprint, err := p.transport()
	if err != nil {
		return Description{}, err
	}
	params, err := agent.StartGathering()
	if err != nil {
		return Description{}, err
	}
	return descriptionFromParams(fingerprint, params), nil
}

// LocalDescriptionSnapshot returns the credentials and candidates gathered so
// far without waiting for gathering completion.
func (p *Participant) LocalDescriptionSnapshot() (Description, error) {
	agent, fingerprint, err := p.transport()
	if err != nil {
		return Description{}, err
	}
	params, err := agent.LocalParamsSnapshot()
	if err != nil {
		return Description{}, err
	}
	return descriptionFromParams(fingerprint, params), nil
}

// LocalCandidateChanges coalesces local candidate arrivals. Candidate values
// remain available only through LocalDescriptionSnapshot.
func (p *Participant) LocalCandidateChanges() <-chan struct{} {
	agent := p.channelAgent()
	if agent == nil {
		return nil
	}
	return agent.CandidateChanges()
}

// LocalGatheringComplete is closed when local ICE gathering finishes.
func (p *Participant) LocalGatheringComplete() <-chan struct{} {
	agent := p.channelAgent()
	if agent == nil {
		return nil
	}
	return agent.GatheringComplete()
}

// Done is closed when the participant transport is closed.
func (p *Participant) Done() <-chan struct{} {
	agent := p.channelAgent()
	if agent == nil {
		return nil
	}
	return agent.Done()
}

// AddRemoteCandidates trickles newly published peer candidates into an
// in-progress Connect. Duplicate and malformed candidates are ignored.
func (p *Participant) AddRemoteCandidates(candidates []string) int {
	agent, _, err := p.transport()
	if err != nil {
		return 0
	}
	return agent.AddRemoteCandidates(candidates)
}

func (p *Participant) LocalDescription(ctx context.Context) (Description, error) {
	agent, fingerprint, err := p.transport()
	if err != nil {
		return Description{}, err
	}
	params, err := agent.LocalParams(ctx)
	if err != nil {
		return Description{}, err
	}
	return descriptionFromParams(fingerprint, params), nil
}

func (p *Participant) Connect(
	ctx context.Context,
	peer Description,
	role Role,
) (*Link, error) {
	if p == nil {
		return nil, errors.New("device-link participant is unavailable")
	}
	if err := validateDescription(peer); err != nil {
		return nil, err
	}
	if role != RoleCaller && role != RoleOwner {
		return nil, fmt.Errorf("unsupported device-link role %q", role)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	p.mu.Lock()
	if p.closed || p.agent == nil {
		p.mu.Unlock()
		return nil, errors.New("device-link participant is closed")
	}
	if p.connectStarted {
		p.mu.Unlock()
		return nil, errors.New("device-link participant connection already started")
	}
	connectCtx, cancel := context.WithCancel(ctx)
	p.connectStarted = true
	p.connectCancel = cancel
	agent := p.agent
	identity := p.identity
	protocols := append([]string(nil), p.protocols...)
	p.mu.Unlock()

	link, err := connectLink(connectCtx, agent, identity, protocols, peer, role)
	cancel()
	if err != nil {
		_ = agent.Close()
		return nil, err
	}

	p.mu.Lock()
	p.connectCancel = nil
	if p.closed {
		p.mu.Unlock()
		_ = link.Close()
		return nil, errors.New("device-link participant closed while connecting")
	}
	p.link = link
	p.mu.Unlock()
	return link, nil
}

func (p *Participant) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	cancel := p.connectCancel
	link := p.link
	agent := p.agent
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if link != nil {
		return link.Close()
	}
	if agent != nil {
		return agent.Close()
	}
	return nil
}

type Link struct {
	agent    *icequic.Agent
	endpoint *devicelink.QUICEndpoint
	session  *devicelink.QUICSession
	scope    string
	once     sync.Once
}

func (l *Link) OpenStream(ctx context.Context) (net.Conn, error) {
	if l == nil || l.session == nil {
		return nil, errors.New("device-link session is closed")
	}
	return l.session.OpenStream(ctx)
}

func (l *Link) AcceptStream(ctx context.Context) (net.Conn, error) {
	if l == nil || l.session == nil {
		return nil, errors.New("device-link session is closed")
	}
	return l.session.AcceptStream(ctx)
}

func (l *Link) SelectedScope() string {
	if l == nil || strings.TrimSpace(l.scope) == "" {
		return icequic.ScopePrivateNetwork
	}
	return l.scope
}

func (l *Link) NegotiatedProtocol() string {
	if l == nil || l.session == nil {
		return ""
	}
	return l.session.NegotiatedProtocol()
}

func (l *Link) Close() error {
	if l == nil {
		return nil
	}
	var closeErr error
	l.once.Do(func() {
		closeErr = errors.Join(l.session.Close(), l.endpoint.Close(), l.agent.Close())
	})
	return closeErr
}

func connectLink(
	ctx context.Context,
	agent *icequic.Agent,
	identity devicelink.Identity,
	protocols []string,
	peer Description,
	role Role,
) (*Link, error) {
	path, err := agent.Connect(
		ctx,
		peer.Ufrag,
		peer.Pwd,
		peer.Candidates,
		role == RoleCaller,
	)
	if err != nil {
		return nil, err
	}
	endpoint, err := devicelink.NewQUICEndpointFromPacketConn(path)
	if err != nil {
		_ = path.Close()
		return nil, err
	}

	var session *devicelink.QUICSession
	if role == RoleCaller {
		tlsConfig, configErr := identity.ClientTLSConfigForProtocols(peer.Fingerprint, protocols)
		if configErr != nil {
			_ = endpoint.Close()
			return nil, configErr
		}
		session, err = endpoint.Dial(ctx, path.RemoteAddr(), tlsConfig)
	} else {
		tlsConfig, configErr := identity.ServerTLSConfigForProtocols(peer.Fingerprint, protocols)
		if configErr != nil {
			_ = endpoint.Close()
			return nil, configErr
		}
		listener, listenErr := endpoint.Listen(tlsConfig)
		if listenErr != nil {
			_ = endpoint.Close()
			return nil, listenErr
		}
		session, err = listener.Accept(ctx)
		_ = listener.Close()
	}
	if err != nil {
		_ = endpoint.Close()
		return nil, err
	}
	return &Link{
		agent: agent, endpoint: endpoint, session: session, scope: agent.SelectedScope(),
	}, nil
}

func validateDescription(description Description) error {
	fingerprint, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimSpace(description.Fingerprint))
	if err != nil || len(fingerprint) != 32 {
		return errors.New("valid peer device-link fingerprint is required")
	}
	if strings.TrimSpace(description.Ufrag) == "" || strings.TrimSpace(description.Pwd) == "" {
		return errors.New("peer device-link ICE credentials are required")
	}
	return nil
}

func (p *Participant) transport() (*icequic.Agent, string, error) {
	if p == nil {
		return nil, "", errors.New("device-link participant is unavailable")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.agent == nil {
		return nil, "", errors.New("device-link participant is closed")
	}
	return p.agent, p.identity.Fingerprint, nil
}

func (p *Participant) channelAgent() *icequic.Agent {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.agent
}

func descriptionFromParams(fingerprint string, params icequic.LocalParams) Description {
	return Description{
		Fingerprint: fingerprint,
		Ufrag:       params.Ufrag,
		Pwd:         params.Pwd,
		Candidates:  append([]string(nil), params.Candidates...),
	}
}
