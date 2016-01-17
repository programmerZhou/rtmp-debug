package main

import (
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/golang/glog"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/google/gopacket/tcpassembly"
)

var (
	pcapFile      = flag.String("f", "", "pcap file from tcpdump")
	pcapInterface = flag.String("i", "all", "interface to read packets from (\"en4\", \"eth0\", ..)")
	snapLen       = flag.Int("s", 65535, "interface snap length")
	promiscOff    = flag.Bool("p", false, "disable promiscuous mode")
)

func printResults(done chan struct{}, results chan string) {
	for x := range results {
		if len(x) > 0 {
			fmt.Printf("%v\n", x)
		}
	}
	close(done)
}

func openPcap() (h *pcap.Handle, err error) {
	if len(*pcapFile) > 0 {
		h, err = pcap.OpenOffline(*pcapFile)
		if err != nil {
			glog.Errorf("unable to open \"%s\"", *pcapFile)
		}
		return
	}

	h, err = pcap.OpenLive(*pcapInterface,
		int32(*snapLen),
		!*promiscOff,
		pcap.BlockForever)
	if err != nil {
		glog.Errorf("unable to open interface \"%s\"", *pcapInterface)
	}
	return
}

func main() {
	flag.Parse()

	if len(flag.Args()) > 0 {
		flag.PrintDefaults()
		os.Exit(2)
	}

	pcapfile, err := openPcap()
	if err != nil {
		glog.Fatalf("%v", err)
	}

	// "Pass this stream factory to an tcpassembly.StreamPool ,
	// start up an tcpassembly.Assembler, and you're good to go!"

	done := make(chan struct{})
	results := make(chan string)
	go printResults(done, results)

	wg := &sync.WaitGroup{}
	rtmp := &rtmpStreamWrapper{wg, results}
	pool := tcpassembly.NewStreamPool(rtmp)
	asm := tcpassembly.NewAssembler(pool)
	asm.MaxBufferedPagesTotal = 4096 // limit gopacket memory allocation

	source := gopacket.NewPacketSource(pcapfile, pcapfile.LinkType())

	for pkt := range source.Packets() {
		if pkt == nil {
			break
		}
		if tcp := pkt.Layer(layers.LayerTypeTCP); tcp != nil {
			asm.AssembleWithTimestamp(
				pkt.TransportLayer().TransportFlow(),
				tcp.(*layers.TCP),
				pkt.Metadata().Timestamp)
		}
	}

	asm.FlushAll() // abort any in progress tcp connections
	wg.Wait()      // tcp streams have finished processing
	close(results) // no more results will be generated by tcp streams
	<-done         // printResults has finished
}