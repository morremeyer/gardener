// Copyright (c) 2021 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package seed

import (
	"context"
	"fmt"

	"github.com/gardener/gardener/pkg/admissioncontroller/webhooks/auth/seed/graph"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime/schema"
	auth "k8s.io/apiserver/pkg/authorization/authorizer"
)

// AuthorizerName is the name of this authorizer.
const AuthorizerName = "seedauthorizer"

// NewAuthorizer returns a new authorizer for requests from gardenlets. It never has an opinion on the request.
func NewAuthorizer(logger logr.Logger, graph graph.Interface) *authorizer {
	return &authorizer{
		logger: logger,
		graph:  graph,
	}
}

type authorizer struct {
	logger logr.Logger
	graph  graph.Interface
}

var _ = auth.Authorizer(&authorizer{})

var (
	// Only take v1beta1 because the Authorize function only checks the resource group and the resource.
	cloudProfileResource = gardencorev1beta1.Resource("cloudprofiles")
)

// TODO: Revisit all `DecisionNoOpinion` later. Today we cannot deny the request for backwards compatibility
// because older Gardenlet versions might not be compatible at the time this authorization plugin is enabled.
// With `DecisionNoOpinion`, RBAC will be respected in the authorization chain afterwards.
func (a *authorizer) Authorize(_ context.Context, attrs auth.Attributes) (auth.Decision, string, error) {
	seedName, isSeed := Identity(attrs.GetUser())
	if !isSeed {
		// reject requests from non-seeds
		return auth.DecisionNoOpinion, "", nil
	}

	if attrs.IsResourceRequest() {
		requestResource := schema.GroupResource{Group: attrs.GetAPIGroup(), Resource: attrs.GetResource()}
		switch requestResource {
		case cloudProfileResource:
			return a.authorizeGet(seedName, graph.VertexTypeCloudProfile, attrs)
		}
	}

	return auth.DecisionNoOpinion, "", nil
}

// authorizeGet authorizes "get" requests to objects of the specified type if they are related to the specified seed
func (a *authorizer) authorizeGet(seedName string, fromType graph.VertexType, attrs auth.Attributes) (auth.Decision, string, error) {
	if attrs.GetVerb() != "get" {
		a.logger.Info(fmt.Sprintf("SEED DENY: '%s' %#v", seedName, attrs))
		return auth.DecisionNoOpinion, "can only get individual resources of this type", nil
	}
	if len(attrs.GetSubresource()) > 0 {
		a.logger.Info(fmt.Sprintf("SEED DENY: '%s' %#v", seedName, attrs))
		return auth.DecisionNoOpinion, "cannot get subresource", nil
	}
	return a.authorize(seedName, fromType, attrs)
}

func (a *authorizer) authorize(seedName string, fromType graph.VertexType, attrs auth.Attributes) (auth.Decision, string, error) {
	if len(attrs.GetName()) == 0 {
		a.logger.Info(fmt.Sprintf("SEED DENY: '%s' %#v", seedName, attrs))
		return auth.DecisionNoOpinion, "No Object name found", nil
	}

	// Allow request if seed name is not known because a target seed cannot be used to find a path.
	if seedName == "" {
		return auth.DecisionAllow, "", nil
	}

	if !a.graph.HasPathFrom(fromType, attrs.GetNamespace(), attrs.GetName(), graph.VertexTypeSeed, "", seedName) {
		a.logger.Info(fmt.Sprintf("SEED DENY: '%s' %#v", seedName, attrs))
		return auth.DecisionNoOpinion, fmt.Sprintf("no relationship found between seed '%s' and this object", seedName), nil
	}

	return auth.DecisionAllow, "", nil
}
