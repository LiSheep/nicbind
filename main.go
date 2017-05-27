package main

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"strconv"
	"sort"
	"strings"
	"os"
	"errors"
	"github.com/BurntSushi/toml"
)

type nicConfig struct {
	Test int `toml:"test"`
	Rx_queues_enable bool
	Rx_queues_begin int
	Rx_queues_end int
	Rx_queues_step int
	Rps_enable bool
	Rps_begin int
	Rps_end int
	Rps_step int
}
type generalConfig struct {
	Reverse_queues bool
	Cpu map[string]nicConfig
}

type NicBindConfig struct {
	General generalConfig
}

type Dev struct {
	Name string
	ints []int
	queue_num int
}

//const INTERRUPTS_FILE = "./interrupts"
const INTERRUPTS_FILE = "/proc/interrupts"
const NET_DIR = "/sys/class/net/"
//const NET_DIR = "./net/"

var g_config NicBindConfig
var interrupts []byte
func findDevIsReal(dev string) bool {
	var err error
	if len(interrupts) == 0 {
		interrupts, err = ioutil.ReadFile(INTERRUPTS_FILE)
		if err != nil {
			panic(err)
		}
	}
	reg, err := regexp.Compile(" " + dev)
	if err != nil {
		panic(err)
	}
	return reg.Match(interrupts)
}

func getDevInterrupts(dev *Dev) {
	dev.ints = make([]int, 0, 3)
	var err error
	if len(interrupts) == 0 {
		fmt.Println("read")
		interrupts, err = ioutil.ReadFile(INTERRUPTS_FILE)
		if err != nil {
			panic(err)
		}
	}
	lines := strings.Split(string(interrupts), "\n")
	reg := regexp.MustCompile("^\\s*(\\d*):[\\s*\\d*]*\\s*.*" + dev.Name + "-")
	for _, line := range(lines) {
		machines := reg.FindStringSubmatch(line)
		if len(machines) != 0 {
			inter, err := strconv.Atoi(machines[1])
			if err != nil {
				panic(err)
			}
			dev.ints = append(dev.ints, inter)
		}
	}
}

func GetRealDev() []Dev {
	fs, err := ioutil.ReadDir(NET_DIR)
	if err != nil {
		panic(err)
	}
	devs := make([]Dev, 0, 2)
	for _, f := range(fs) {
		if findDevIsReal(f.Name()) {
			dev := Dev{Name:f.Name()}
			getDevInterrupts(&dev)
			dev.queue_num = len(dev.ints)
			devs = append(devs, dev)
		}
	}

	return devs
}

func getCpuNum() int {
	b, err := ioutil.ReadFile("/proc/cpuinfo")
	if err != nil {
		panic(err)
	}
	cpuCount := 0
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {

		if len(line) == 0 && i != len(lines)-1 {
			cpuCount++
			continue
		} else if i == len(lines)-1 {
			continue
		}
	}
	return cpuCount
}

func getCpuMask(id int) string {
	return strconv.FormatUint(1 << uint32(id), 16)
}

func getGeneralNicConfigKey(cpuNums int) string {
	var keys []int
	for cpu := range(g_config.General.Cpu) {
		c, err := strconv.Atoi(cpu)
		if err != nil {
			fmt.Println(err)
			return ""
		}
		keys = append(keys, c)
	}
	sort.Ints(keys)
	for _, key := range(keys) {
		if cpuNums <= key {
			return strconv.Itoa(key)
		}
	}
	return ""
}

func buildNicConfig(c *nicConfig) (error) {
	cpuNums := getCpuNum()
	if c.Rx_queues_begin >= cpuNums {
		return errors.New("rx_queues_begin too big")
	}
	if c.Rx_queues_end > cpuNums || c.Rx_queues_end <= 0 {
		c.Rx_queues_end = cpuNums
	}
	if c.Rps_begin >= cpuNums {
		return errors.New("rps_begin too big")
	}
	if c.Rps_end > cpuNums || c.Rps_end <= 0 {
		c.Rps_end = cpuNums
	}
	return nil
}

func getInterrupts(devs []Dev) []int {
	ints := make([]int, 0, 10)
	for _, dev := range(devs) {
		fmt.Printf("process %s, queues num: %d \n", dev.Name, len(dev.ints))
		for i := range(dev.ints) {
			ints = append(ints, dev.ints[i])
		}
	}
	return ints
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("argument error")
		return
	}
	if _, err := toml.DecodeFile(os.Args[1], &g_config); err != nil {
		fmt.Println(err)
		return
	}
	// get real dev
	devs := GetRealDev()

	// get cpu
	cpuNums := getCpuNum()
	fmt.Println("current cpu num:", cpuNums)

	// get general config
	gNicConfig := g_config.General.Cpu[getGeneralNicConfigKey(cpuNums)]
	err := buildNicConfig(&gNicConfig)
	if  err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(gNicConfig, devs)
	for _, d := range(devs) {
		fmt.Printf("dev: %s queues num: %d", d.Name, d.queue_num)
	}
	incr := true
	ints := getInterrupts(devs)
	id := gNicConfig.Rx_queues_begin
	if gNicConfig.Rx_queues_enable {
		for _, irq := range(ints) {
			f, err := os.OpenFile("/proc/irq/" + strconv.Itoa(irq) +"/smp_affinity",
				//f, err := os.OpenFile("./irq/" + strconv.Itoa(irq) +"/smp_affinity",
				os.O_WRONLY|os.O_TRUNC, os.ModePerm)
			if err != nil {
				panic(err)
			}
			fmt.Println("bind id :", id)
			f.Write([]byte(getCpuMask(id)))
			f.Close()
			if g_config.General.Reverse_queues {
				if incr {
					id++
					if id == gNicConfig.Rx_queues_end {
						id = gNicConfig.Rx_queues_end - 1
						incr = false
					}
				} else {
					id--
					if id == gNicConfig.Rx_queues_begin - 1 {
						id = gNicConfig.Rx_queues_begin
						incr = true
					}
				}
			} else {
				id++
				if id == gNicConfig.Rx_queues_end {
					id = gNicConfig.Rx_queues_begin
				}
			}
		}
	} else {
		fmt.Println("unbind rx-queues")
		for _, irq := range(ints) {
			f, err := os.OpenFile("/proc/irq/" + strconv.Itoa(irq) +"/smp_affinity",
				//f, err := os.OpenFile("./irq/" + strconv.Itoa(irq) +"/smp_affinity",
				os.O_WRONLY|os.O_TRUNC, os.ModePerm)
			if err != nil {
				panic(err)
			}
			f.Write([]byte("0"))
			f.Close()
		}
	}
	for _, dev := range(devs) {
		queues_dir := NET_DIR + dev.Name + "/queues/"
		fs, err := ioutil.ReadDir(queues_dir)
		if err != nil {
			panic(err)
		}
		// bins irq
		if gNicConfig.Rps_enable {
			fmt.Println("bind rps")
			id := gNicConfig.Rps_begin
			for _, f := range(fs) {
				if strings.Contains(f.Name(), "tx") {
					continue
				}
				file, err := os.OpenFile(queues_dir + f.Name() + "/rps_cpus",
					os.O_WRONLY|os.O_TRUNC, os.ModePerm)
				if err != nil {
					panic(err)
				}
				file.Write([]byte(getCpuMask(id % gNicConfig.Rps_end)))
				file.Close()
				id += gNicConfig.Rps_step
				if id == gNicConfig.Rps_end {
					id = gNicConfig.Rps_begin
				}
			}
		} else {
			fmt.Println("unbind rps")
			for _, f := range(fs) {
				if strings.Contains(f.Name(), "tx") {
					continue
				}
				file, err := os.OpenFile(queues_dir + f.Name() + "/rps_cpus",
					os.O_WRONLY|os.O_TRUNC, os.ModePerm)
				if err != nil {
					panic(err)
				}
				file.Write([]byte("0"))
				file.Close()
			}
		}
	}
}
