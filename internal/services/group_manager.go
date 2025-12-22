package services

import (
	"context"
	"encoding/json"
	"fmt"
	"gpt-load/internal/config"
	"gpt-load/internal/jsonengine"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
	"gpt-load/internal/syncer"
	"gpt-load/internal/utils"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const GroupUpdateChannel = "groups:updated"

// GroupManager manages the caching of group data.
type GroupManager struct {
	syncer          *syncer.CacheSyncer[map[string]*models.Group]
	db              *gorm.DB
	store           store.Store
	settingsManager *config.SystemSettingsManager
	subGroupManager *SubGroupManager
}

// NewGroupManager creates a new, uninitialized GroupManager.
func NewGroupManager(
	db *gorm.DB,
	store store.Store,
	settingsManager *config.SystemSettingsManager,
	subGroupManager *SubGroupManager,
) *GroupManager {
	return &GroupManager{
		db:              db,
		store:           store,
		settingsManager: settingsManager,
		subGroupManager: subGroupManager,
	}
}

// Initialize sets up the CacheSyncer. This is called separately to handle potential
func (gm *GroupManager) Initialize() error {
	loader := func() (map[string]*models.Group, error) {
		var groups []*models.Group
		if err := gm.db.Find(&groups).Error; err != nil {
			return nil, fmt.Errorf("failed to load groups from db: %w", err)
		}

		// Load all sub-group relationships for aggregate groups (only valid ones with weight > 0)
		var allSubGroups []models.GroupSubGroup
		if err := gm.db.Where("weight > 0").Find(&allSubGroups).Error; err != nil {
			return nil, fmt.Errorf("failed to load valid sub groups: %w", err)
		}

		// Group sub-groups by aggregate group ID
		subGroupsByAggregateID := make(map[uint][]models.GroupSubGroup)
		for _, sg := range allSubGroups {
			subGroupsByAggregateID[sg.GroupID] = append(subGroupsByAggregateID[sg.GroupID], sg)
		}

		// Create group ID to group object mapping for sub-group lookups
		groupByID := make(map[uint]*models.Group)
		for _, group := range groups {
			groupByID[group.ID] = group
		}

		groupMap := make(map[string]*models.Group, len(groups))
		for _, group := range groups {
			g := *group
			g.EffectiveConfig = gm.settingsManager.GetEffectiveConfig(g.Config)
			g.ProxyKeysMap = utils.StringToSet(g.ProxyKeys, ",")

			// Parse header rules with error handling
			if len(group.HeaderRules) > 0 {
				if err := json.Unmarshal(group.HeaderRules, &g.HeaderRuleList); err != nil {
					logrus.WithError(err).WithField("group_name", g.Name).Warn("Failed to parse header rules for group")
					g.HeaderRuleList = []models.HeaderRule{}
				}
			} else {
				g.HeaderRuleList = []models.HeaderRule{}
			}

			// Parse inbound rules (request body transformation)
			if len(group.InboundRules) > 0 {
				if err := json.Unmarshal(group.InboundRules, &g.InboundRuleList); err != nil {
					logrus.WithError(err).WithField("group_name", g.Name).Warn("Failed to parse inbound rules for group")
					g.InboundRuleList = []jsonengine.Rule{}
				}
			} else {
				g.InboundRuleList = []jsonengine.Rule{}
			}

			// Parse outbound rules (response body transformation)
			if len(group.OutboundRules) > 0 {
				if err := json.Unmarshal(group.OutboundRules, &g.OutboundRuleList); err != nil {
					logrus.WithError(err).WithField("group_name", g.Name).Warn("Failed to parse outbound rules for group")
					g.OutboundRuleList = []jsonengine.Rule{}
				}
			} else {
				g.OutboundRuleList = []jsonengine.Rule{}
			}

			// Parse model redirect rules with weight support
			g.ModelRedirectMap = make(map[string][]models.ModelRedirectTarget)
			if len(group.ModelRedirectRules) > 0 {
				hasInvalidRules := false
				for key, value := range group.ModelRedirectRules {
					// value should be []any, each element is map[string]any with "model" and "weight"
					if targets, ok := value.([]any); ok {
						var redirectTargets []models.ModelRedirectTarget
						for _, t := range targets {
							if targetMap, ok := t.(map[string]any); ok {
								model, modelOk := targetMap["model"].(string)
								weight, weightOk := targetMap["weight"].(float64) // JSON numbers are float64
								if modelOk && weightOk && weight > 0 {
									redirectTargets = append(redirectTargets, models.ModelRedirectTarget{
										Model:  model,
										Weight: int(weight),
									})
								}
							}
						}
						if len(redirectTargets) > 0 {
							g.ModelRedirectMap[key] = redirectTargets
						}
					} else {
						logrus.WithFields(logrus.Fields{
							"group_name": g.Name,
							"rule_key":   key,
							"value_type": fmt.Sprintf("%T", value),
						}).Error("Invalid model redirect rule format, expected array of targets")
						hasInvalidRules = true
					}
				}
				if hasInvalidRules {
					logrus.WithField("group_name", g.Name).Warn("Group has invalid model redirect rules, some rules were skipped")
				}
			}

			// Load sub-groups for aggregate groups
			if g.GroupType == "aggregate" {
				if subGroups, ok := subGroupsByAggregateID[g.ID]; ok {
					g.SubGroups = make([]models.GroupSubGroup, len(subGroups))
					for i, sg := range subGroups {
						g.SubGroups[i] = sg
						if subGroup, exists := groupByID[sg.SubGroupID]; exists {
							g.SubGroups[i].SubGroupName = subGroup.Name
						}
					}
				}
			}

			groupMap[g.Name] = &g
			logrus.WithFields(logrus.Fields{
				"group_name":                 g.Name,
				"effective_config":           g.EffectiveConfig,
				"header_rules_count":         len(g.HeaderRuleList),
				"inbound_rules_count":        len(g.InboundRuleList),
				"outbound_rules_count":       len(g.OutboundRuleList),
				"model_redirect_rules_count": len(g.ModelRedirectMap),
				"model_redirect_strict":      g.ModelRedirectStrict,
				"sub_group_count":            len(g.SubGroups),
			}).Debug("Loaded group with effective config")
		}

		return groupMap, nil
	}

	afterReload := func(newCache map[string]*models.Group) {
		gm.subGroupManager.RebuildSelectors(newCache)
	}

	syncer, err := syncer.NewCacheSyncer(
		loader,
		gm.store,
		GroupUpdateChannel,
		logrus.WithField("syncer", "groups"),
		afterReload,
	)
	if err != nil {
		return fmt.Errorf("failed to create group syncer: %w", err)
	}
	gm.syncer = syncer
	return nil
}

// GetGroupByName retrieves a single group by its name from the cache.
func (gm *GroupManager) GetGroupByName(name string) (*models.Group, error) {
	if gm.syncer == nil {
		return nil, fmt.Errorf("GroupManager is not initialized")
	}

	groups := gm.syncer.Get()
	group, ok := groups[name]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return group, nil
}

// Invalidate triggers a cache reload across all instances.
func (gm *GroupManager) Invalidate() error {
	if gm.syncer == nil {
		return fmt.Errorf("GroupManager is not initialized")
	}
	return gm.syncer.Invalidate()
}

// Stop gracefully stops the GroupManager's background syncer.
func (gm *GroupManager) Stop(ctx context.Context) {
	if gm.syncer != nil {
		gm.syncer.Stop()
	}
}
