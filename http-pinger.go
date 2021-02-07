package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/aeden/traceroute"
	"io/ioutil"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"
)

var (
	c      = make(chan string)
	Config ConfigJson
	sourceIp, err = http.Get("https://api.ipify.org")
	options = traceroute.TracerouteOptions{}
)

type ConfigJson struct {
	Lag          int      `json:"lag"`
	Interval     int      `json:"interval"`
	UrlsFile     string   `json:"urls_file"`
	SmtpUsername string   `json:"smtp_username"`
	SmtpPassword string   `json:"smtp_password"`
	SmtpHost     string   `json:"smtp_host"`
	SmtpPort     string   `json:"smtp_port"`
	EmailSubject string   `json:"email_subject"`
	FromEmail    string   `json:"from_email"`
	ToEmails     []string `json:"to_emails"`
}

type Msg struct {
	Source    string `json:"source_ip"`
	Server    string `json:"server_url"`
	Type      string `json:"message_type"`
	Date      string `json:"date"`
	Lag       int    `json:"lag_threshold_in_second"`
	Interval  int    `json:"check_interval_in_second"`
	Status    int    `json:"http_status_code"`
	Responsed string `json:"server_responsed_in"`
	Error     string `json:"error"`
}


func main() {

	var m = flag.Int("m", 20, `Set the max time-to-live (max number of hops) used in outgoing probe packets (default is 64)`)
	var f = flag.Int("f", traceroute.DEFAULT_FIRST_HOP, `Set the first used time-to-live, e.g. the first hop (default is 1)`)
	var q = flag.Int("q", 1, `Set the number of probes per "ttl" to nqueries (default is one probe).`)

	flag.Parse()
	// host := flag.Arg(0)
	// options := traceroute.TracerouteOptions{}
	options.SetRetries(*q - 1)
	options.SetMaxHops(*m + 1)
	options.SetFirstHop(*f)


	ParseConfig()
	file_data, err := ioutil.ReadFile(Config.UrlsFile)
	if err != nil {
		panic(err)
	}
	var urls []string
	for _, line := range strings.Split(string(file_data), "\n") {
		url := strings.TrimSpace(line)
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			urls = append(urls, url)
		}
	}


	var source = httpGet("https://api.ipify.org/")

	for _, url := range urls {
		fmt.Println(url)
		go ping(url,source)
	}

	for {
		select {
		case msg := <-c:

			fmt.Println(msg)
		}
	}
}

func ping(urlString string, source string) {
	for {
		start := time.Now()

		url2, urlerr := url.Parse(urlString)
		if urlerr != nil {
			panic(urlerr)
		}

		res, err := http.Get(urlString)

		msg := Msg{}
		if err != nil {
			msg.Source = source
			msg.Server = urlString
			msg.Type = "Fatal"
			msg.Date = time.Now().Format("2006-01-02 15:04 -0700")
			msg.Lag = Config.Lag
			msg.Interval = Config.Interval
			msg.Status = -1
			msg.Responsed = "NaN"
			msg.Error = err.Error()
			b, _ := json.Marshal(msg)
			str := string(b)
			EmailMsg(str)
			tracerouteForHost(url2.Host)

			c <- str
		} else {
			lag := time.Since(start)
			msg.Source = source
			msg.Server = url2.Host
			msg.Date = time.Now().Format("2006-01-02 15:04 -0700")
			msg.Lag = Config.Lag
			msg.Interval = Config.Interval
			msg.Status = res.StatusCode
			msg.Responsed = lag.String()
			msg.Type = "OK" // expected

			if res.StatusCode != 200 {
				msg.Type = "Warning"
				msg.Error = "Unexpected http status code!"
				b, _ := json.Marshal(msg)
				str := string(b)
				EmailMsg(str)
				tracerouteForHost(url2.Host)


			}
			if lag > time.Duration(Config.Lag)*time.Second {
				msg.Type = "Warning"
				msg.Error = "Responsed times over lag threshold!"
				b, _ := json.Marshal(msg)
				str := string(b)
				EmailMsg(str)
				tracerouteForHost(url2.Host)

			}
			b, _ := json.Marshal(msg)
			str := string(b)
			c <- str
			res.Body.Close()
		}
		time.Sleep(time.Duration(Config.Interval) * time.Second)
	}
}

func EmailMsg(msg string) {
	// sendMail(Config.EmailSubject, msg, Config.FromEmail, Config.ToEmails)
}

func sendMail(subject string, message string, from string, to []string) {
	auth := smtp.PlainAuth(
		"",
		Config.SmtpUsername,
		Config.SmtpPassword,
		Config.SmtpHost,
	)
	msg := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: %s\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s", strings.Join(to, ";"), from, subject, message)
	err := smtp.SendMail(fmt.Sprintf("%s:%s", Config.SmtpHost, Config.SmtpPort), auth, from, to, []byte(msg))
	if err != nil {
		fmt.Println("[Warning] Send Email failed: ", err.Error())
		return
	}

	fmt.Println("Sent Email Notification to: ", to)
}

func ParseConfig() {
	conf := os.Getenv("CONFIG")
	if conf == "" {
		conf = "config.json"
	}
	file, err := os.Open(conf)
	if err != nil {
		fmt.Println("Read config.json failed: ", err.Error())
		os.Exit(1)
	}
	defer file.Close()
	j := json.NewDecoder(file)
	err = j.Decode(&Config)
	if err != nil {
		fmt.Println("Parse config.json failed: ", err.Error())
		os.Exit(1)
	}
}

func printHop(hop traceroute.TracerouteHop) {
	addr := fmt.Sprintf("%v.%v.%v.%v", hop.Address[0], hop.Address[1], hop.Address[2], hop.Address[3])
	hostOrAddr := addr
	if hop.Host != "" {
		hostOrAddr = hop.Host
	}
	if hop.Success {
		fmt.Printf("%-3d %v (%v)  %v\n", hop.TTL, hostOrAddr, addr, hop.ElapsedTime)
	} else {
		fmt.Printf("%-3d *\n", hop.TTL)
	}
}


func tracerouteForHost(host string) {



	ipAddr, err := net.ResolveIPAddr("ip", host)
	if err != nil {
		return
	}

	fmt.Printf("traceroute to %v (%v), %v hops max, %v byte packets\n", host, ipAddr, options.MaxHops(), options.PacketSize())

	c := make(chan traceroute.TracerouteHop, 0)
	go func() {
		for {
			hop, ok := <-c
			if !ok {
				fmt.Println()
				return
			}
			printHop(hop)
		}
	}()

	_, err = traceroute.Traceroute(host, &options, c)
	if err != nil {
		fmt.Printf("Error: ", err)
	}

}


func httpGet( url string) string {
	response, err := http.Get(url)
	if err != nil {
		fmt.Printf("%s", err)
		os.Exit(1)
	} else {
		defer response.Body.Close()
		contents, err := ioutil.ReadAll(response.Body)
		if err != nil {
			fmt.Printf("%s", err)
			os.Exit(1)
		}
		fmt.Printf("%s\n", string(contents))
		return string(contents)
	}
	return ""
}
