package neural

import (
	"math/rand"
	"testing"
	"time"
)

func TestLayer(t *testing.T) {
	rand.Seed(time.Now().Unix())

	in := 10
	out := 2

	invec := make([]float64, in)
	for i := 0; i < in; i++ {
		invec[i] = 1.0
	}

	l := NewLayer(in, out, LeakyReLu{})
	outvec := l.Forward(invec)

	t.Fatalf("outvec: %v", outvec)
}

func TestLayer_MutateAndBreed(t *testing.T) {
	rand.Seed(time.Now().Unix())

	in := 10
	out := 2

	l1 := NewLayer(in, out, Tanh{})

	invec := make([]float64, in)
	for i := 0; i < in; i++ {
		invec[i] = 1.0
	}

	outvec1 := l1.Forward(invec)

	l2 := l1.Mutate(0.1)

	outvec2 := l2.Forward(invec)

	t.Logf("outvec1: %v", outvec1)
	t.Logf("outvec2: %v", outvec2)
	distance1 := Distance(outvec1, outvec2)
	t.Logf("distance1: %f", distance1)

	l1.Breed(l2, 0.1)

	outvec3 := l1.Forward(invec)
	t.Logf("outvec3: %v", outvec3)
	distance2 := Distance(outvec1, outvec3)
	t.Logf("distance2: %f", distance2)

	t.Errorf("delta[distance]: %f", distance2-distance1)
}

func TestNetwork(t *testing.T) {
	sizes := []int{5, 10, 5}

	rand.Seed(time.Now().Unix())

	net := NewNetwork(sizes)

	input := make([]float64, sizes[0])
	for i := 0; i < sizes[0]; i++ {
		input[i] = float64(i) + 1.0
	}

	out := net.Forward(input)

	t.Fatalf("out: %v", out)
}

func TestNetwork_Train(t *testing.T) {
	target := []float64{0.5, 1.0, -1.0, 0}
	sizes := []int{5, 10, 10, 4}

	rand.Seed(time.Now().Unix())

	net := NewNetwork(sizes)

	input := make([]float64, sizes[0])
	for i := 0; i < sizes[0]; i++ {
		input[i] = 1.0
	}

	var bestSoFar Network
	initialRate := 10.0
	rate := initialRate
	roundsWithoutImprovement := 0
	for round := 0; round < 10000; round++ {
		outPreTrain := net.Forward(input)
		net.Train(input, target, 150, rate)
		outPostTrain := net.Forward(input)

		dist1 := Distance(outPreTrain, target)
		dist2 := Distance(outPostTrain, target)

		delta := dist1 - dist2
		if delta > 0 {
			t.Logf("round %d rate: %f dist1: %f dist2: %f delta: %f", round, rate, dist1, dist2, delta)
			bestSoFar = net.Copy()
			roundsWithoutImprovement = 0
		} else {
			roundsWithoutImprovement++
			rate *= 0.95
			if rate < 1e-5 {
				rate = 1e-5
			}
		}

		if roundsWithoutImprovement > 100 {
			t.Logf("reset in round %d, rate: %f", round, rate)

			rate = initialRate
			net = bestSoFar
			roundsWithoutImprovement = 0
		}
	}

	outPostTrain := bestSoFar.Forward(input)

	distance := Distance(outPostTrain, target)
	t.Logf("after training: rate: %f distance: %f output: %v", rate, distance, outPostTrain)

	t.Error("a")
}
