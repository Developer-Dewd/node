/*
 * Copyright (C) 2018 The "MysteriumNetwork/node" Authors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"

	tequilapi_client "github.com/mysteriumnetwork/node/tequilapi/client"
)

var (
	consumerPassphrase = "localconsumer"
	providerID         = "0xd1a23227bd5ad77f36ba62badcb78a410a1db6c5"
	providerPassphrase = "localprovider"
	accountantID       = "0xf2e2c77D2e7207d8341106E6EfA469d1940FD0d8"
)

func TestConsumerConnectsToProvider(t *testing.T) {
	tequilapiProvider := newTequilapiProvider()
	tequilapiConsumer := newTequilapiConsumer()
	t.Run("ProviderRegistersIdentityFlow", func(t *testing.T) {
		providerRegistrationFlow(t, tequilapiProvider, providerID, providerPassphrase)
	})

	var consumerID string
	// no need to register provider, as he will auto-register
	t.Run("ConsumerCreatesAndRegistersIdentityFlow", func(t *testing.T) {
		consumerID = identityCreateFlow(t, tequilapiConsumer, consumerPassphrase)
		consumerRegistrationFlow(t, tequilapiConsumer, consumerID, consumerPassphrase)
	})

	t.Run("ConsumerConnectFlow", func(t *testing.T) {
		servicesInFlag := strings.Split(*consumerServices, ",")
		for _, serviceType := range servicesInFlag {
			if _, ok := serviceTypeAssertionMap[serviceType]; ok {
				t.Run(serviceType, func(t *testing.T) {
					proposal := consumerPicksProposal(t, tequilapiConsumer, serviceType)
					balanceSpent := consumerConnectFlow(t, tequilapiConsumer, consumerID, accountantID, serviceType, proposal)
					providerEarnedTokens(t, tequilapiProvider, providerID, balanceSpent)
				})
			}
		}
	})
}

func identityCreateFlow(t *testing.T, tequilapi *tequilapi_client.Client, idPassphrase string) string {
	id, err := tequilapi.NewIdentity(idPassphrase)
	assert.NoError(t, err)
	log.Info().Msg("Created new identity: " + id.Address)

	return id.Address
}

func providerRegistrationFlow(t *testing.T, tequilapi *tequilapi_client.Client, id, idPassphrase string) {
	err := tequilapi.Unlock(id, idPassphrase)
	assert.NoError(t, err)

	idStatus, err := tequilapi.Identity(id)
	assert.NoError(t, err)
	assert.Equal(t, "RegisteredProvider", idStatus.RegistrationStatus)
	assert.Equal(t, "0xD4bf8ac88E7Ad1f777a084EEfD7Be4245E0b4eD3", idStatus.ChannelAddress)
	assert.Equal(t, uint64(690000000), idStatus.Balance)
	assert.Zero(t, idStatus.Earnings)
	assert.Zero(t, idStatus.EarningsTotal)
}

func consumerRegistrationFlow(t *testing.T, tequilapi *tequilapi_client.Client, id, idPassphrase string) {
	err := tequilapi.Unlock(id, idPassphrase)
	assert.NoError(t, err)

	fees, err := tequilapi.GetTransactorFees()
	assert.NoError(t, err)

	err = tequilapi.RegisterIdentity(id, id, 0, fees.Registration)
	assert.NoError(t, err)

	// now we check identity again
	err = waitForCondition(func() (bool, error) {
		regStatus, err := tequilapi.IdentityRegistrationStatus(id)
		return regStatus.Registered, err
	})
	assert.NoError(t, err)

	idStatus, err := tequilapi.Identity(id)
	assert.NoError(t, err)
	assert.Equal(t, "RegisteredConsumer", idStatus.RegistrationStatus)
	assert.Equal(t, uint64(690000000), idStatus.Balance)
	assert.Zero(t, idStatus.Earnings)
	assert.Zero(t, idStatus.EarningsTotal)
}

// expect exactly one proposal
func consumerPicksProposal(t *testing.T, tequilapi *tequilapi_client.Client, serviceType string) tequilapi_client.ProposalDTO {
	var proposals []tequilapi_client.ProposalDTO
	err := waitForConditionFor(
		30*time.Second,
		func() (state bool, stateErr error) {
			proposals, stateErr = tequilapi.ProposalsByType(serviceType)
			return len(proposals) == 1, stateErr
		},
	)
	if err != nil {
		assert.FailNowf(t, "Exactly one proposal is expected - something is not right!", "Error was: %v", err)
	}

	log.Info().Msgf("Selected proposal is: %v, serviceType=%v", proposals[0], serviceType)
	return proposals[0]
}

func consumerConnectFlow(t *testing.T, tequilapi *tequilapi_client.Client, consumerID, accountantID, serviceType string, proposal tequilapi_client.ProposalDTO) uint64 {
	err := topUpAccount(consumerID)
	assert.Nil(t, err)

	connectionStatus, err := tequilapi.ConnectionStatus()
	assert.NoError(t, err)
	assert.Equal(t, "NotConnected", connectionStatus.Status)

	nonVpnIP, err := tequilapi.ConnectionIP()
	assert.NoError(t, err)
	log.Info().Msg("Original consumer IP: " + nonVpnIP)

	err = waitForCondition(func() (bool, error) {
		status, err := tequilapi.ConnectionStatus()
		return status.Status == "NotConnected", err
	})
	assert.NoError(t, err)

	connectionStatus, err = tequilapi.ConnectionCreate(consumerID, proposal.ProviderID, accountantID, serviceType, tequilapi_client.ConnectOptions{
		DisableKillSwitch: false,
	})

	assert.NoError(t, err)

	err = waitForCondition(func() (bool, error) {
		status, err := tequilapi.ConnectionStatus()
		return status.Status == "Connected", err
	})
	assert.NoError(t, err)

	vpnIP, err := tequilapi.ConnectionIP()
	assert.NoError(t, err)
	log.Info().Msg("Changed consumer IP: " + vpnIP)

	// sessions history should be created after connect
	sessionsDTO, err := tequilapi.ConnectionSessionsByType(serviceType)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(sessionsDTO.Sessions))
	se := sessionsDTO.Sessions[0]
	assert.Zero(t, se.Duration)
	assert.Zero(t, se.BytesSent)
	assert.Zero(t, se.BytesReceived)
	assert.Equal(t, "e2e-land", se.ProviderCountry)
	assert.Equal(t, serviceType, se.ServiceType)
	assert.Equal(t, proposal.ProviderID, se.ProviderID)
	assert.Equal(t, connectionStatus.SessionID, se.SessionID)
	assert.Equal(t, "New", se.Status)

	// Wait some time for session to collect stats.
	if serviceType != "noop" {
		assert.Eventually(t, sessionStatsReceived(tequilapi), 30*time.Second, 1*time.Second)
	}

	err = tequilapi.ConnectionDestroy()
	assert.NoError(t, err)

	err = waitForCondition(func() (bool, error) {
		status, err := tequilapi.ConnectionStatus()
		return status.Status == "NotConnected", err
	})
	assert.NoError(t, err)

	// sessions history should be updated after disconnect
	sessionsDTO, err = tequilapi.ConnectionSessionsByType(serviceType)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(sessionsDTO.Sessions))
	se = sessionsDTO.Sessions[0]
	assert.Equal(t, "Completed", se.Status)

	// call the custom asserter for the given service type
	serviceTypeAssertionMap[serviceType](t, se)

	consumerStatus, err := tequilapi.Identity(consumerID)
	assert.NoError(t, err)
	assert.True(t, consumerStatus.Balance < uint64(690000000), "balance should decrease but is %sd", consumerStatus.Balance)
	assert.Zero(t, consumerStatus.Earnings)
	assert.Zero(t, consumerStatus.EarningsTotal)

	return uint64(690000000) - consumerStatus.Balance
}

func providerEarnedTokens(t *testing.T, tequilapi *tequilapi_client.Client, id string, earningsExpected uint64) {
	// Before settlement
	providerStatus, err := tequilapi.Identity(id)
	assert.NoError(t, err)
	assert.Equal(t, uint64(690000000), providerStatus.Balance)
	assert.Equal(t, earningsExpected, providerStatus.Earnings)
	assert.Equal(t, earningsExpected, providerStatus.EarningsTotal)
	// TODO Compare with sessions stats + proposal price
	assert.True(t, providerStatus.Earnings > uint64(500), "earnings should be at least 500 but is %d", providerStatus.Earnings)

	// TODO Implement settlement test here
}

func sessionStatsReceived(tequilapi *tequilapi_client.Client) func() bool {
	return func() bool {
		stats, err := tequilapi.ConnectionStatistics()
		if err != nil {
			return false
		}
		return stats.BytesReceived > 0 && stats.BytesSent > 0
	}
}

type sessionAsserter func(t *testing.T, session tequilapi_client.ConnectionSessionDTO)

var serviceTypeAssertionMap = map[string]sessionAsserter{
	"openvpn": func(t *testing.T, session tequilapi_client.ConnectionSessionDTO) {
		assert.NotZero(t, session.Duration)
		assert.NotZero(t, session.BytesSent)
		assert.NotZero(t, session.BytesReceived)
	},
	"noop": func(t *testing.T, session tequilapi_client.ConnectionSessionDTO) {
		assert.NotZero(t, session.Duration)
		assert.Zero(t, session.BytesSent)
		assert.Zero(t, session.BytesReceived)
	},
	"wireguard": func(t *testing.T, session tequilapi_client.ConnectionSessionDTO) {
		assert.NotZero(t, session.Duration)
		assert.NotZero(t, session.BytesSent)
		assert.NotZero(t, session.BytesReceived)
	},
}
