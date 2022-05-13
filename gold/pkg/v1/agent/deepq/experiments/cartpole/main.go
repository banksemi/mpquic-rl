package main

import (
	"github.com/aunum/gold/pkg/v1/agent/deepq"
	"github.com/aunum/gold/pkg/v1/common"
	"github.com/aunum/gold/pkg/v1/common/require"
	envv1 "github.com/aunum/gold/pkg/v1/env"
	"github.com/aunum/log"
)

func main() {
	s, err := envv1.NewLocalServer(envv1.GymServerConfig)
	require.NoError(err)
	defer s.Close()

	env, err := s.Make("CartPole-v0",
		envv1.WithNormalizer(envv1.NewExpandDimsNormalizer(0)),
	)
	require.NoError(err)

	agent, err := deepq.NewAgent(deepq.DefaultAgentConfig)
	require.NoError(err)

	agent.View()

	numEpisodes := 200
	agent.Epsilon = common.DefaultDecaySchedule(common.WithDecayRate(0.9995))

	episodes:= agent.MakeEpisodes(numEpisodes)

	episode := episodes[0]
	for i := 0; i <= 300; i++ {
		init, err := env.Reset()
		require.NoError(err)

		state := init.Observation

		// score := episode.TrackScalar("score", 0, track.WithAggregator(track.Max))
		// timesteps:= episode.Steps(env.MaxSteps())
		for j := 0; j <= env.MaxSteps(); j++ {
			// timestep := timesteps[j]
			action, err := agent.Action(state)
			require.NoError(err)

			outcome, err := env.Step(action)
			require.NoError(err)

			// score.Inc(outcome.Reward)

			log.Successf("Episode %d finished %f", episode.I, outcome.Reward)
			event := deepq.NewEvent(state, action, outcome)
			agent.Remember(event)

			err = agent.Learn()
			require.NoError(err)

			if outcome.Done {
				log.Successf("Episode %d finished", episode.I)
				break
			}
			state = outcome.Observation

			err = agent.Render(env)
			require.NoError(err)
		}
		episode.Log()
	}
	agent.Wait()
	env.End()
}
