package factory

import (
	"github.com/concourse/atc"
	"github.com/concourse/atc/db"
)

const defaultTaskName = "build"

type BuildFactory struct {
	ConfigDB db.ConfigDB
}

func (factory *BuildFactory) Create(
	job atc.JobConfig,
	resources atc.ResourceConfigs,
	inputs []db.BuildInput,
) (atc.Plan, error) {
	return factory.constructPlanSequenceBasedPlan(job.Plan, resources, inputs), nil
}

func (factory *BuildFactory) constructPlanSequenceBasedPlan(
	planSequence atc.PlanSequence,
	resources atc.ResourceConfigs,
	inputs []db.BuildInput,
) atc.Plan {
	if len(planSequence) == 0 {
		return atc.Plan{}
	}

	// work backwards to simplify conditional wrapping
	plan := factory.constructPlanFromConfig(
		planSequence[len(planSequence)-1],
		resources,
		inputs,
	)

	for i := len(planSequence) - 1; i > 0; i-- {
		// plan preceding the current one in the sequence
		prevPlan := factory.constructPlanFromConfig(
			planSequence[i-1],
			resources,
			inputs,
		)

		// steps default to conditional on [success]
		plan = makeConditionalOnSuccess(plan)

		// if the previous plan is conditional, make the entire following chain
		// of composed steps conditional
		plan = conditionallyCompose(prevPlan, plan)
	}

	return plan
}

func makeConditionalOnSuccess(plan atc.Plan) atc.Plan {
	if plan.Conditional != nil {
		return plan
	} else if plan.Aggregate != nil {
		conditionaled := atc.AggregatePlan{}
		for name, plan := range *plan.Aggregate {
			conditionaled[name] = makeConditionalOnSuccess(plan)
		}

		plan.Aggregate = &conditionaled
	} else {
		plan = atc.Plan{
			Conditional: &atc.ConditionalPlan{
				Conditions: atc.Conditions{atc.ConditionSuccess},
				Plan:       plan,
			},
		}
	}

	return plan
}

func conditionallyCompose(prevPlan atc.Plan, plan atc.Plan) atc.Plan {
	if prevPlan.Conditional != nil {
		plan = atc.Plan{
			Conditional: &atc.ConditionalPlan{
				Conditions: prevPlan.Conditional.Conditions,
				Plan: atc.Plan{
					Compose: &atc.ComposePlan{
						A: prevPlan.Conditional.Plan,
						B: plan,
					},
				},
			},
		}
	} else {
		plan = atc.Plan{
			Compose: &atc.ComposePlan{
				A: prevPlan,
				B: plan,
			},
		}
	}

	return plan
}

func (factory *BuildFactory) constructPlanFromConfig(
	planConfig atc.PlanConfig,
	resources atc.ResourceConfigs,
	inputs []db.BuildInput,
) atc.Plan {
	var plan atc.Plan

	switch {
	case planConfig.Do != nil:
		plan = factory.constructPlanSequenceBasedPlan(
			*planConfig.Do,
			resources,
			inputs,
		)

	case planConfig.Get != "":
		resourceName := planConfig.Resource
		if resourceName == "" {
			resourceName = planConfig.Get
		}

		resource, _ := resources.Lookup(resourceName)

		name := planConfig.Get
		var version db.Version
		for _, input := range inputs {
			if input.Name == name {
				version = input.Version
				break
			}
		}

		plan = atc.Plan{
			Get: &atc.GetPlan{
				Type:     resource.Type,
				Name:     name,
				Resource: resourceName,
				Source:   resource.Source,
				Params:   planConfig.Params,
				Version:  atc.Version(version),
			},
		}

	case planConfig.Put != "":
		resourceName := planConfig.Resource
		if resourceName == "" {
			resourceName = planConfig.Put
		}

		resource, _ := resources.Lookup(resourceName)

		plan = atc.Plan{
			Put: &atc.PutPlan{
				Type:     resource.Type,
				Name:     planConfig.Put,
				Resource: resourceName,
				Source:   resource.Source,
				Params:   planConfig.Params,
			},
		}

	case planConfig.Task != "":
		plan = atc.Plan{
			Task: &atc.TaskPlan{
				Name:       planConfig.Task,
				Privileged: planConfig.Privileged,
				Config:     planConfig.TaskConfig,
				ConfigPath: planConfig.TaskConfigPath,
			},
		}

	case planConfig.Aggregate != nil:
		aggregate := atc.AggregatePlan{}

		for _, planConfig := range *planConfig.Aggregate {
			aggregate[planConfig.Name()] = factory.constructPlanFromConfig(
				planConfig,
				resources,
				inputs,
			)
		}

		plan = atc.Plan{
			Aggregate: &aggregate,
		}
	}

	if planConfig.Conditions != nil {
		plan = atc.Plan{
			Conditional: &atc.ConditionalPlan{
				Conditions: *planConfig.Conditions,
				Plan:       plan,
			},
		}
	}

	return plan
}
