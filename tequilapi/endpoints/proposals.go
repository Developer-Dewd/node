/*
 * Copyright (C) 2017 The "MysteriumNetwork/node" Authors.
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

package endpoints

import (
	"net/http"
	"strconv"

	"github.com/julienschmidt/httprouter"
	"github.com/mysteriumnetwork/node/core/discovery/proposal"
	"github.com/mysteriumnetwork/node/core/quality"
	"github.com/mysteriumnetwork/node/market"
	"github.com/mysteriumnetwork/node/money"
	"github.com/mysteriumnetwork/node/tequilapi/utils"
)

// swagger:model ProposalsList
type proposalsRes struct {
	Proposals []*proposalDTO `json:"proposals"`
}

// swagger:model ServiceLocationDTO
type locationRes struct {
	// example: EU
	Continent string `json:"continent,omitempty"`
	// example: NL
	Country string `json:"country,omitempty"`
	// example: Amsterdam
	City string `json:"city,omitempty"`

	// Autonomous System Number
	// example: 00001
	ASN int `json:"asn"`
	// example: Telia Lietuva, AB
	ISP string `json:"isp,omitempty"`
	// example: residential
	NodeType string `json:"node_type,omitempty"`
}

// swagger:model ServiceDefinitionDTO
type serviceDefinitionRes struct {
	LocationOriginate locationRes `json:"location_originate"`
}

type metricsRes struct {
	ConnectCount quality.ConnectCount `json:"connect_count"`
}

type paymentRateRes struct {
	PerSeconds uint64 `json:"per_seconds"`
	PerBytes   uint64 `json:"per_bytes"`
}

type paymentMethodRes struct {
	Type  string         `json:"type"`
	Price money.Money    `json:"price"`
	Rate  paymentRateRes `json:"rate"`
}

// swagger:model ProposalDTO
type proposalDTO struct {
	// per provider unique serial number of service description provided
	// example: 5
	ID int `json:"id"`

	// provider who offers service
	// example: 0x0000000000000000000000000000000000000001
	ProviderID string `json:"provider_id"`

	// type of service provider offers
	// example: openvpn
	ServiceType string `json:"service_type"`

	// qualitative service definition
	ServiceDefinition serviceDefinitionRes `json:"service_definition"`

	// Metrics of the service
	Metrics *metricsRes `json:"metrics,omitempty"`

	// AccessPolicies
	AccessPolicies *[]market.AccessPolicy `json:"access_policies,omitempty"`

	// PaymentMethod
	PaymentMethod paymentMethodRes `json:"payment_method"`
}

func proposalToRes(p market.ServiceProposal) *proposalDTO {
	return &proposalDTO{
		ID:          p.ID,
		ProviderID:  p.ProviderID,
		ServiceType: p.ServiceType,
		ServiceDefinition: serviceDefinitionRes{
			LocationOriginate: locationRes{
				Continent: p.ServiceDefinition.GetLocation().Continent,
				Country:   p.ServiceDefinition.GetLocation().Country,
				City:      p.ServiceDefinition.GetLocation().City,

				ASN:      p.ServiceDefinition.GetLocation().ASN,
				ISP:      p.ServiceDefinition.GetLocation().ISP,
				NodeType: p.ServiceDefinition.GetLocation().NodeType,
			},
		},
		AccessPolicies: p.AccessPolicies,
		PaymentMethod: paymentMethodRes{
			Type:  p.PaymentMethod.GetType(),
			Price: p.PaymentMethod.GetPrice(),
			Rate: paymentRateRes{
				PerSeconds: uint64(p.PaymentMethod.GetRate().PerTime.Seconds()),
				PerBytes:   p.PaymentMethod.GetRate().PerByte,
			},
		},
	}
}

// QualityFinder allows to fetch proposal quality data
type QualityFinder interface {
	ProposalsMetrics() []quality.ConnectMetric
}

type proposalsEndpoint struct {
	proposalRepository proposal.Repository
	qualityProvider    QualityFinder
}

// NewProposalsEndpoint creates and returns proposal creation endpoint
func NewProposalsEndpoint(proposalRepository proposal.Repository, qualityProvider QualityFinder) *proposalsEndpoint {
	return &proposalsEndpoint{
		proposalRepository: proposalRepository,
		qualityProvider:    qualityProvider,
	}
}

// swagger:operation GET /proposals Proposal listProposals
// ---
// summary: Returns proposals
// description: Returns list of proposals filtered by provider id
// parameters:
//   - in: query
//     name: provider_id
//     description: id of provider proposals
//     type: string
//   - in: query
//     name: service_type
//     description: the service type of the proposal. Possible values are "openvpn", "wireguard" and "noop"
//     type: string
//   - in: query
//     name: access_policy_id
//     description: the access policy id to filter the proposals by
//     type: string
//   - in: query
//     name: access_policy_source
//     description: the access policy source to filter the proposals by
//     type: string
//   - in: query
//     name: fetch_connect_counts
//     description: if set to true, fetches the connection success metrics for nodes. False by default.
//     type: boolean
// responses:
//   200:
//     description: List of proposals
//     schema:
//       "$ref": "#/definitions/ProposalsList"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
func (pe *proposalsEndpoint) List(resp http.ResponseWriter, req *http.Request, params httprouter.Params) {
	fetchConnectCounts := req.URL.Query().Get("fetch_connect_counts")

	upperTimePriceBound, err := parsePriceBound(req, "upper_time_price_bound")
	if err != nil {
		utils.SendError(resp, err, http.StatusBadRequest)
		return
	}
	lowerTimePriceBound, err := parsePriceBound(req, "lower_time_price_bound")
	if err != nil {
		utils.SendError(resp, err, http.StatusBadRequest)
		return
	}

	upperGBPriceBound, err := parsePriceBound(req, "upper_gb_price_bound")
	if err != nil {
		utils.SendError(resp, err, http.StatusBadRequest)
		return
	}
	lowerGBPriceBound, err := parsePriceBound(req, "lower_gb_price_bound")
	if err != nil {
		utils.SendError(resp, err, http.StatusBadRequest)
		return
	}

	proposals, err := pe.proposalRepository.Proposals(&proposal.Filter{
		ProviderID:          req.URL.Query().Get("provider_id"),
		ServiceType:         req.URL.Query().Get("service_type"),
		AccessPolicyID:      req.URL.Query().Get("access_policy_id"),
		AccessPolicySource:  req.URL.Query().Get("access_policy_source"),
		LowerGBPriceBound:   lowerGBPriceBound,
		UpperGBPriceBound:   upperGBPriceBound,
		LowerTimePriceBound: lowerTimePriceBound,
		UpperTimePriceBound: upperTimePriceBound,
		ExcludeUnsupported:  true,
	})

	if err != nil {
		utils.SendError(resp, err, http.StatusInternalServerError)
		return
	}

	proposalsRes := proposalsRes{Proposals: []*proposalDTO{}}
	for _, p := range proposals {
		proposalsRes.Proposals = append(proposalsRes.Proposals, proposalToRes(p))
	}

	if fetchConnectCounts == "true" {
		metrics := pe.qualityProvider.ProposalsMetrics()
		addProposalMetrics(proposalsRes.Proposals, metrics)
	}

	utils.WriteAsJSON(proposalsRes, resp)
}

func parsePriceBound(req *http.Request, key string) (*uint64, error) {
	bound := req.URL.Query().Get(key)
	if bound == "" {
		return nil, nil
	}
	upperPriceBound, err := strconv.ParseUint(req.URL.Query().Get(key), 10, 64)
	return &upperPriceBound, err
}

// AddRoutesForProposals attaches proposals endpoints to router
func AddRoutesForProposals(router *httprouter.Router, proposalRepository proposal.Repository, qualityProvider QualityFinder) {
	pe := NewProposalsEndpoint(proposalRepository, qualityProvider)
	router.GET("/proposals", pe.List)
}

// addProposalMetrics adds quality metrics to proposals.
func addProposalMetrics(proposals []*proposalDTO, metrics []quality.ConnectMetric) {
	// Convert metrics slice to map for fast lookup.
	metricsMap := map[string]quality.ConnectMetric{}
	for _, m := range metrics {
		metricsMap[m.ProposalID.ProviderID+m.ProposalID.ServiceType] = m
	}

	for _, p := range proposals {
		if mc, ok := metricsMap[p.ProviderID+p.ServiceType]; ok {
			p.Metrics = &metricsRes{ConnectCount: mc.ConnectCount}
		}
	}
}
