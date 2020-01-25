package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rekognition"
	pixela "github.com/gainings/pixela-go-client"
)

// Point is left & top positions of bounding box in the Rekognition result
type Point struct {
	Left float64
	Top  float64
}

// Request is assumed request body from API Gateway
type Request struct {
	URL string `json:"url"`
}

// !! fixed number from experiment (maybe require to change your env) !!
var assumedDatePoint = Point{Left: 0.311, Top: 0.063}
var assumedActivityTimePoint = Point{Left: 0.339, Top: 0.183}
var assumedCaloriePoint = Point{Left: 0.531, Top: 0.183}
var assumedDistancePoint = Point{Left: 0.742, Top: 0.183}

// Handler is our lambda handler invoked by the `lambda.Start` function call
func Handler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	fmt.Printf("Processing request data for request %s.\n", request.RequestContext.RequestID)

	// extract env var
	user := os.Getenv("PIXELA_USER")
	token := os.Getenv("PIXELA_TOKEN")
	actgraph := os.Getenv("PIXELA_ACTTIME_GRAPH")
	calgraph := os.Getenv("PIXELA_CAL_GRAPH")
	distgraph := os.Getenv("PIXELA_DIST_GRAPH")

	// extract url from request
	var reqbody Request
	err := json.Unmarshal([]byte(request.Body), &reqbody)
	if err != nil {
		fmt.Println(err.Error())
		return events.APIGatewayProxyResponse{Body: "Record failed", StatusCode: 500}, err
	}
	url := reqbody.URL
	fmt.Printf("Posted URL: %s\n", url)

	// extract image url
	imgURLs, err := getImageURL(url)
	if err != nil {
		fmt.Println(err.Error())
		return events.APIGatewayProxyResponse{Body: "Record failed", StatusCode: 500}, err
	}
	fmt.Printf("Image URL: %s\n", imgURLs)

	for _, imgURL := range imgURLs {
		// extract image bytes
		img, err := getImage(imgURL)
		if err != nil {
			fmt.Println(err.Error())
			return events.APIGatewayProxyResponse{Body: "Record failed", StatusCode: 500}, err
		}

		// execute text detection of Rekognition
		res, err := exeRekognitionDetectText(img)
		if err != nil {
			fmt.Println(err.Error())
			return events.APIGatewayProxyResponse{Body: "Record failed", StatusCode: 500}, err
		}

		// extract date & quantities from the above result
		date, acttime, cal, dist := getValueFromRekognitionResult(res.TextDetections)
		fmt.Printf("date: %s, acttime: %s, cal: %s, dist: %s\n", date, acttime, cal, dist)

		// record pixel
		err = recordPixel(user, token, actgraph, date, acttime)
		err = recordPixel(user, token, calgraph, date, cal)
		err = recordPixel(user, token, distgraph, date, dist)
		if err != nil {
			fmt.Println(err)
			return events.APIGatewayProxyResponse{Body: "Record failed", StatusCode: 500}, err
		}
	}

	return events.APIGatewayProxyResponse{Body: "Successfully recorded", StatusCode: 200}, nil
}

func getImageURL(url string) ([]string, error) {
	const TargetProp = "og:image"
	imgURLs := []string{}

	res, err := http.Get(url)
	if err != nil {
		return imgURLs, err
	}

	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return imgURLs, err
	}

	doc.Find("meta").Each(func(i int, s *goquery.Selection) {
		p, e := s.Attr("property")
		if e && p == TargetProp {
			imgURL, _ := s.Attr("content")
			imgURLs = append(imgURLs, imgURL)
		}
	})

	if len(imgURLs) == 0 {
		err = errors.New("no image url in input url")
	} else {
		err = nil
	}

	return imgURLs, err
}

func getImage(url string) ([]byte, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	fmt.Println(res.StatusCode, res.Status)

	imgdata, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return imgdata, nil
}

func exeRekognitionDetectText(img []byte) (*rekognition.DetectTextOutput, error) {
	// create Rekognition client
	sess := session.Must(session.NewSession())
	rc := rekognition.New(sess, aws.NewConfig().WithRegion("ap-northeast-1"))

	// set params
	params := &rekognition.DetectTextInput{
		Image: &rekognition.Image{
			Bytes: img,
		},
	}
	fmt.Printf("params: %s", params)

	// execute DetectText
	return rc.DetectText(params)
}

func getValueFromRekognitionResult(results []*rekognition.TextDetection) (string, string, string, string) {
	dateHypot, acttimeHypot, calHypot, distHypot := math.MaxFloat64, math.MaxFloat64, math.MaxFloat64, math.MaxFloat64
	date, acttime, cal, dist := "", "", "", ""

	// for each detected text
	for _, td := range results {
		// check WORD since assumed**Point is asuume WORD box
		if *td.Type != "WORD" {
			continue
		}

		dateHypot, date = updateHypot(td, assumedDatePoint, dateHypot, date)
		acttimeHypot, acttime = updateHypot(td, assumedActivityTimePoint, acttimeHypot, acttime)
		calHypot, cal = updateHypot(td, assumedCaloriePoint, calHypot, cal)
		distHypot, dist = updateHypot(td, assumedDistancePoint, distHypot, dist)

	}

	// formatting
	fmt.Printf("before format %s, %s, %s, %s\n", date, acttime, cal, dist)
	date = setDateStr(date)
	acttime = timecode2min(acttime)
	cal = strings.Replace(cal, "kcal", "", 1)
	dist = strings.Replace(dist, "km", "", 1)

	return date, acttime, cal, dist
}

func updateHypot(td *rekognition.TextDetection, assumedPoint Point, hypot float64, candidate string) (float64, string) {
	left, top := *td.Geometry.BoundingBox.Left, *td.Geometry.BoundingBox.Top

	// calc hypot with assumed pos & update value
	tmpHypot := math.Hypot(math.Abs(left-assumedPoint.Left), math.Abs(top-assumedPoint.Top))
	if tmpHypot < hypot {
		// if td is most-likely-result (nearest to the assumed point), keep the result
		hypot, candidate = tmpHypot, *td.DetectedText
	}

	return hypot, candidate
}

func setDateStr(original string) string {
	// formatting "mm/dd" -> "yyyymmdd"
	res := strconv.Itoa(time.Now().Year()) + strings.Replace(original, "/", "", -1)
	tmpDate, _ := time.Parse("20060102", res)
	fmt.Printf("original date: %s\n", res)

	now := time.Now()
	if tmpDate.After(now) {
		// if tmpDate is future, alternatively use oen-year-before date
		res = strconv.Itoa(time.Now().Year()-1) + strings.Replace(original, "/", "", -1)
	}
	fmt.Printf("result: %s\n", res)

	return res
}

func timecode2min(original string) string {
	sp := []float64{}
	for _, s := range strings.Split(original, ":") {
		f, _ := strconv.ParseFloat(s, 64)
		sp = append(sp, f)
	}

	min := fmt.Sprintf("%.2f", sp[0]*60+sp[1]+sp[2]/60)

	return min
}

func recordPixel(user, token, graph, date, quantity string) error {
	c := pixela.NewClient(user, token)

	// try to record
	err := c.UpdatePixelQuantity(graph, date, quantity)
	if err == nil {
		fmt.Println("updated")
	}

	return err
}

func main() {
	lambda.Start(Handler)
}
