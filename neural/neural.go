package neural

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"

	"gonum.org/v1/gonum/mat"
)

type Nonlinearity interface {
	Forward(float64) float64
}

type Tanh struct{}

func (t Tanh) Forward(v float64) float64 {
	return math.Tanh(v)
}

type LeakyReLu struct {
	leak float64
}

func (r LeakyReLu) Forward(v float64) float64 {
	if v < 0 {
		return v * r.leak
	}

	return v
}

type Layer struct {
	nonlinearity Nonlinearity

	in  int
	out int

	weights *mat.Dense
}

// NewLayer returns a new layer with the given number of input/output neurons and randomized weights
func NewLayer(in, out int, nonlinearity Nonlinearity) Layer {
	in++ // Add additional input for bias

	scale := math.Sqrt(2.0 / float64(in))

	m := mat.NewDense(out, in, nil)
	m.Apply(func(r, c int, v float64) float64 {
		return rand.NormFloat64() * scale
	}, m)

	return Layer{
		nonlinearity: nonlinearity,
		out:          out,
		in:           in,
		weights:      m,
	}
}

func (l Layer) Breed(other Layer, inheritance float64) {
	if l.out == 0 || l.in == 0 {
		panic("weird layer size")
	}

	l.weights.Apply(func(r, c int, v float64) float64 {
		return other.weights.At(r, c)*(1-inheritance) + v*inheritance
	}, l.weights)
}

func (l Layer) Copy() Layer {
	if l.out == 0 || l.in == 0 {
		panic("weird layer size")
	}

	weights := mat.NewDense(l.out, l.in, make([]float64, l.out*l.in))
	weights.Copy(l.weights)

	return Layer{
		in:           l.in,
		out:          l.out,
		weights:      weights,
		nonlinearity: l.nonlinearity,
	}
}

func (l Layer) Forward(in []float64) []float64 {
	if l.out == 0 || l.in == 0 {
		panic("weird layer size")
	}

	in = append(in, 1.0) // Append bias

	input := mat.NewVecDense(len(in), in)

	v := mat.NewVecDense(l.out, nil)

	v.MulVec(l.weights, input)

	data := v.RawVector().Data
	for i := 0; i < len(data); i++ {
		data[i] = l.nonlinearity.Forward(data[i])
	}

	return data
}

func (l Layer) Mutate(rate float64) Layer {
	if l.out == 0 || l.in == 0 {
		panic("weird layer size")
	}

	m := mat.NewDense(l.out, l.in, nil)
	m.Apply(func(r, c int, v float64) float64 {
		f := rand.Float64()
		if f > rate {
			f = 0
		} else {
			f = 1
		}

		return v + rand.NormFloat64()*rate*f
	}, m)

	return Layer{
		in:           l.in,
		out:          l.out,
		nonlinearity: l.nonlinearity,
		weights:      m,
	}
}

func (l Layer) String() string {
	if l.out == 0 || l.in == 0 {
		panic("weird layer size")
	}

	return fmt.Sprintf("Layer: {%v}", l.weights.RawMatrix())
}

type Network struct {
	layers []Layer
}

func NewNetwork(sizes []int) Network {
	layers := make([]Layer, len(sizes)-1)

	for i := 0; i < len(layers); i++ {
		layer := NewLayer(sizes[i], sizes[i+1], Tanh{})
		layers[i] = layer
		if layer.out == 0 || layer.in == 0 {
			panic("weird layer size")
		}
	}

	return Network{
		layers: layers,
	}
}

func (n Network) Breed(other Network, inheritance float64) {
	for i, l := range n.layers {
		if l.out == 0 || l.in == 0 {
			panic("weird layer size")
		}

		l.Breed(other.layers[i], inheritance)
	}
}

func (n Network) Copy() Network {
	layers := make([]Layer, len(n.layers))
	for i, l := range n.layers {
		if l.out == 0 || l.in == 0 {
			panic("weird layer size")
		}

		layers[i] = l.Copy()
	}

	return Network{
		layers: layers,
	}
}

func (n Network) Forward(input []float64) []float64 {
	for _, l := range n.layers {
		if l.out == 0 || l.in == 0 {
			panic("weird layer size")
		}

		input = l.Forward(input)
	}

	return input
}

func (n Network) Mutate(rate float64) Network {
	layers := make([]Layer, len(n.layers))
	for i, l := range n.layers {
		if l.out == 0 || l.in == 0 {
			panic("weird layer size")
		}

		layers[i] = l.Mutate(rate)
	}

	return Network{
		layers: layers,
	}
}

func (n Network) String() string {
	res := make([]string, len(n.layers))

	for i, l := range n.layers {
		if l.out == 0 || l.in == 0 {
			panic("weird layer size")
		}

		res[i] = l.String()
	}

	return "Network{" + strings.Join(res, "\n") + "}"
}

func (n *Network) Train(input []float64, output []float64, numClones int, rate float64) {
	sema := make(chan struct{}, 10)

	clones := make([]Network, numClones)
	distances := make([]float64, numClones)
	for i := 0; i < numClones; i++ {
		i := i
		sema <- struct{}{}
		go func() {
			clone := n.Mutate(rate)
			clones[i] = clone
			distances[i] = Distance(output, clone.Forward(input))
			<-sema
		}()
	}

	for i := 0; i < cap(sema); i++ {
		sema <- struct{}{}
	}

	bestDistance := math.MaxFloat64
	bestClone := 0
	for i := 0; i < numClones; i++ {
		if distances[i] < bestDistance {
			bestDistance = distances[i]
			bestClone = i
		}
	}

	currentDistance := Distance(output, n.Forward(input))

	if bestDistance < currentDistance {
		// Determine how many traits to inherit from the original parent. If the clone is vastly better
		// than the original, this tends to just take the clone.
		inheritance := bestDistance / (bestDistance + currentDistance)
		logInheritance := 1 + math.Log(bestDistance/currentDistance)
		log.Println("best", bestDistance, "current", currentDistance, "inheritance:", inheritance, "logInheritance", logInheritance)
		if logInheritance < inheritance {
			inheritance = 0
		}

		n.Breed(clones[bestClone], inheritance)
	}
}

func Distance(v1 []float64, v2 []float64) float64 {
	if len(v1) != len(v2) {
		panic("vector length not equal")
	}

	dist := 0.0
	for i, v := range v1 {
		dist += math.Pow(v-v2[i], 2)
	}

	return math.Sqrt(dist)
}
