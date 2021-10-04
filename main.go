package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	log "github.com/sirupsen/logrus"
	"math"
	"os"
	"strconv"
	_ "time"
)

type record struct {
	accX float64
	accY float64
	accZ float64
}

type epoch struct {
	records []*record
}

type correction struct {
	axis rune

	// offset
	d float64

	// gain factor
	a float64
}

var (
	recordsPerSecond = 30
	g = 9.81
)

var G float64 = 6.67e-11

func main() {
	var threshold float64
	var file string
	var iterations int

	args := flag.NewFlagSet("args", flag.ExitOnError)
	args.StringVar(&file, "f", "", "CSV file to parse.")
	args.Float64Var(&threshold, "t", 0, "Threshold at which the auto-correction is terminated.")
	args.IntVar(&iterations, "n", 1000, "Number of ICP iterations.")
	args.Parse(os.Args[1:])

	if file == "" {
		log.Warnln("File path was not provided. Exiting.")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if threshold <= 0 {
		log.Warnln("Thresold must be a positive floating point number. Exiting.")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if iterations <= 0 {
		log.Warnln("The number of iterations must be greater than zero. Exiting")
		flag.PrintDefaults()
		os.Exit(1)
	}

	records, err := readCSVRecords(file)
	if err != nil {
		log.Fatal(err.Error())
	}

	allEpochs, err := getEpochs(records)
	if err != nil {
		log.Fatal(err.Error())
	}

	// Epochs whose SD < threshold are retained
	epochs, err := preProcessEpochs(allEpochs, threshold)
	if err != nil {
		log.Fatal(err.Error())
	}

	corrections, err := ICP(epochs, threshold, iterations)
	if err != nil {
		log.Fatal(err.Error())
	}

	for _, r := range corrections {
		log.Printf("Axis: %c\tOffset d: %f\tGain factor a: %f\n", r.axis, r.d, r.a)	
	}
}

func ICP(epochs []*epoch, threshold float64, nIterations int) ([]*correction, error) {
	if len(epochs) == 0 {
		return nil, errors.New("No epochs to iterate")
	}

	var dX float64 = 0
	var aX float64 = 1
	var dY float64 = 0
	var aY float64 = 1
	var dZ float64 = 0
	var aZ float64 = 1
	
	for _, e := range epochs {
		weight := 1 - g / math.Abs(e.euclideanNorm() - g)
		if weight >= 100 {
			weight = 100
		}
	
		// TODO more here
		for i := 0; i < nIterations; i++ {
			dX -= weight
			aX -= weight
			dY -= weight
			aY -= weight
			dZ -= weight
			aZ -= weight
		}
	}

	return []*correction{
		&correction{
			axis: 'X',
			d: dX / (float64(nIterations) + float64(len(epochs))),
			a: aX / (float64(nIterations) + float64(len(epochs))),
		},
		&correction{
			axis: 'Y',
			d: dY / (float64(nIterations) + float64(len(epochs))),
			a: aY / (float64(nIterations) + float64(len(epochs))),
		},
		&correction{
			axis: 'Z',
			d: dZ / (float64(nIterations) + float64(len(epochs))),
			a: aZ / (float64(nIterations) + float64(len(epochs))),
		},
	}, nil
}

func (e *epoch) euclideanNorm() float64 {
	meanX, meanY, meanZ := e.mean()
	log.Println("len epoch:", len(e.records))
	return math.Sqrt(math.Pow(meanX, 2) + math.Pow(meanY, 2) + math.Pow(meanZ, 2))
}

// Pre-computes the records
func preProcessEpochs(epochs []*epoch, threshold float64) ([]*epoch, error) {
	if len(epochs) == 0 {
		return nil, errors.New("No epochs to pre-process")
	}

	processed := make([]*epoch, 0)

	for _, e := range epochs {
		meanX, meanY, meanZ := e.mean()
		sdX, sdY, sdZ := e.standardDeviation(meanX, meanY, meanZ)

		//log.Println("sdX, sdY, sdZ:", sdX, sdY, sdZ)
		if sdX < threshold && sdY < threshold && sdZ < threshold {
			processed = append(processed, e)
		}
	}

	return processed, nil
}

// Returns an epoch of records that measures nSeconds in time
func getEpochs(records []*record) ([]*epoch, error) {
	// 10 s epochs, assuming 30 Hz frequence
	size := 300
	epochs := make([]*epoch, 0)

	for {
		if len(records) == 0 {
			break
		}

		if len(records) < size {
			size = len(records)
		}

		e := &epoch{
			records: records[0:size],
		}

		epochs = append(epochs, e)
		records = records[size:]
	}

	return epochs, nil
}

func (e *epoch) mean() (float64, float64, float64) {
	var meanX float64 = 0
	var meanY float64 = 0
	var meanZ float64 = 0

	for _, r := range e.records {
		meanX += r.accX
		meanY += r.accY
		meanZ += r.accZ
	}

	l := float64(len(e.records))

	return meanX / l, meanY / l, meanZ / l
}

func (e *epoch) standardDeviation(meanX, meanY, meanZ float64) (float64, float64, float64) {
	var sdX float64 = 0
	var sdY float64 = 0
	var sdZ float64 = 0
	l := float64(len(e.records))

	for _, r := range e.records {
		sdX += math.Pow(r.accX - meanX, 2)
		sdY += math.Pow(r.accY - meanY, 2)
		sdZ += math.Pow(r.accZ - meanZ, 2)
	}

	return math.Sqrt(sdX / l), math.Sqrt(sdY / l), math.Sqrt(sdZ / l)
}

func readCSVRecords(filePath string) ([]*record, error) {
	records := make([]*record, 0)

	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("Unable to read input file at path %s", filePath)
	}
	defer f.Close()

	csvReader := csv.NewReader(f)
	recordsArray, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("Unable to parse file as CSV at path %s", filePath)
	}

	for _, r := range recordsArray {
		x, err := strconv.ParseFloat(r[0], 64)
		if err != nil {
			return nil, err
		}

		y, err := strconv.ParseFloat(r[1], 64)
		if err != nil {
			return nil, err
		}

		z, err := strconv.ParseFloat(r[2], 64)
		if err != nil {
			return nil, err
		}

		rec := &record{
			accX: x,
			accY: y,
			accZ: z,
		}

		records = append(records, rec)
	}

	return records, nil
}
