// This file contains the peer logic. Every peer performs the following functions:
// 1)Connects to relay using TCP for a long lasting connection.
// 2)Shares its info with relay(complete model of peer)
// 3)Get the addresses for availabel peers for connection from the relay.
// 4)Select a peer from the list and tries to get a direct connection from the relay using holepunching
// 5)If successful uses udp defined by libp2p to continue the connection and talk to the peer
// 6)iF fails to do so, it uses the relayed connection only to communicate to that peer
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/multiformats/go-multiaddr"
)

const msgProtoID = protocol.ID("/simplechat/1.0.0")

type Peer struct {
	Host      host.Host
	RelayAddr multiaddr.Multiaddr
	RelayID   peer.ID
	//PeerID peer.ID NO NEED AS HANDELD BY INDETIFY SERVICE CREATED IN THE FUNCTION NEW PEER
}

func NewPeer(relayAddr string) (*Peer, error) {
	//This function creates a basic setup for any peer in the communication setup.

	fmt.Println("[DEBUG] initialising a new peer")
	//first we have to create a multiaddress of relay address as the functions of lib p2p use multiaddr. Every peer requires a realy address so it knows which relay to connect to.
	relayMultiaddr, err := multiaddr.NewMultiaddr(relayAddr)
	if err != nil {
		fmt.Println("Error resolving relay address to a multiaddress")
		return nil, err
	}
	//from relay multiaddr, we create an obj containing all the info for the peer.  This makes accessing the info easier
	relayInfo, err2 := peer.AddrInfoFromP2pAddr(relayMultiaddr)
	if err2 != nil {
		fmt.Println("Error getting info from the relay address")
		return nil, err2
	}

	//every peer in libp2p has a host which manages how the peer manages its connections. It is here only we can define if we want to use relay and hoelpunching for the peer
	host, errHost := libp2p.New(
		libp2p.ListenAddrStrings(""),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.EnableNATService(),
	)

	if errHost != nil {
		fmt.Println("error initialising the host for the  peer")
		return nil, errHost
	}

	// service, errSer:= holepunch.NewService(host) this is oncorrect func call as we also need to pass an ID service and a function that manages public ips for the peer.

	//This func is passed into holepunch service func. Its job is to determine the public IPS availabel to peers on which it can listen. This is very important to pass as without this holepunch service is unaware of the public IPS peer is listening to and thus may not be able to traverse the nat. Earlier it was not a compulasory para, but now it is
	getPubAddrs := func() []multiaddr.Multiaddr {
		var pubAddrs []multiaddr.Multiaddr
		for _, addr := range host.Addrs() {
			if !isPrivateIP(addr) {
				pubAddrs = append(pubAddrs, addr)
			}
		}
		return pubAddrs
	}

	//ID service is also very imp for holepunch func as is manages how peer shares its identity or info with other peers. It includes its ips, it PeerID, its port, its nat type, protocols supported etc etc.
	idService, errID := identify.NewIDService(host)
	if errID != nil {
		fmt.Println("Error creating id service")
		return nil, errID
	}

	service, errSer := holepunch.NewService(host, idService, getPubAddrs)
	_ = service
	if errSer != nil {
		fmt.Println("Error creating holepunch service")
		return nil, errSer
	}

	var ChatPeer Peer
	ChatPeer.Host = host
	ChatPeer.RelayAddr = relayMultiaddr
	ChatPeer.RelayID = relayInfo.ID

	//this func starts a stream for the host we have connected to a peer. A stream can be understtod as the tunnel through which all data travels.Libp2p supports multiplexed streams, ie a peer can manage connections with multiple peers at the same time.
	//It takes the protocol id, which we can make our own and a function that handles streams
	host.SetStreamHandler(msgProtoID, ChatPeer.StreamHandler)
	return &ChatPeer, nil
}

func isPrivateIP(ip multiaddr.Multiaddr) bool {
	addrStr := ip.String()
	if strings.Contains(addrStr, "127.0.0.1") ||
		strings.Contains(addrStr, "192.168.") ||
		strings.Contains(addrStr, "10.") ||
		strings.Contains(addrStr, "172.16.") ||
		strings.Contains(addrStr, "172.17.") ||
		strings.Contains(addrStr, "172.18.") ||
		strings.Contains(addrStr, "172.19.") ||
		strings.Contains(addrStr, "172.2") ||
		strings.Contains(addrStr, "172.30.") ||
		strings.Contains(addrStr, "172.31.") {
		return true
	}
	return false

}

func (cp Peer) StreamHandler(s network.Stream) {
	//this fnc is passed into SetStream we made above as the stream handler for any peer. It simply reads data on the stream from any peer. We dont have to set up all the routines and all as that is managed by libp2p.
	remotePeer := s.Conn().RemotePeer()
	fmt.Println("[DEBUG] Getting data from stream via remote peer: ", remotePeer)

	buf := make([]byte, 256)
	n, err := s.Read(buf) // wraps the info it gets from the stream onto buf which is a alice of bytes.
	if err != nil {
		fmt.Println("[ERROR] Failed to read from stream:", err)
		return
	}

	msg := string(buf[:n])
	fmt.Println("[INFO] Received message:", msg)
}

func (cp Peer) ConnectToRelay(ctx context.Context) error {
	fmt.Println("[DEBUG]Trying to connect to Relay of address", cp.RelayAddr.String())
	relayInfo, err := peer.AddrInfoFromP2pAddr(cp.RelayAddr)
	if err != nil {
		fmt.Println("Error parsing relay info")
		return err
	}
	err = cp.Host.Connect(ctx, *relayInfo)
	if err != nil {
		fmt.Println("Error connecting to given relay")
		return err
	}
	fmt.Println("[DEBUG] ESTABLISHED CONNECTION WITH THE RELAY Peer ID is ", cp.Host.ID())
	return nil
}

func (cp Peer) ConnectToPeer(ctx context.Context, peerIP string) error {
	peerAddr, err := multiaddr.NewMultiaddr(peerIP)
	if err != nil {
		fmt.Println("[DEBUG]Error creating peer multiaddr")
	}
	fmt.Println("Trying to connect to peer with addrs", peerAddr.String())
	peerInfo, err := peer.AddrInfoFromP2pAddr(peerAddr)
	if err != nil {
		fmt.Println("[DEBUG]Error parsing peer info")
		return err
	}
	err = cp.Host.Connect(ctx, *peerInfo)
	if err != nil {
		fmt.Println("[DEBUG]Error connecting to the peer with address", peerAddr.String())
		return err
	}

	fmt.Printf("[DEBUG] Connected to %s!\n", peerIP)

	conns := cp.Host.Network().ConnsToPeer(peerInfo.ID) //identifies number of conections with the peer
	if len(conns) > 0 {
		fmt.Printf("[DEBUG] Connection type: %s\n", conns[0].RemoteMultiaddr()) //gives the type of connectin establishe dwith that peer
	} else {
		fmt.Println("[DEBUG] No connection found after connect")
	}
	return nil
}

func (cp *Peer) SendMessage(peerID peer.ID, message string) error {
	fmt.Printf("[DEBUG] Sending message to %s: %s\n", peerID, message)
	stream, err := cp.Host.NewStream(context.Background(), peerID, msgProtoID)
	if err != nil {
		fmt.Println("[DEBUG] Failed to open stream:", err)
		return err
	}
	defer stream.Close()

	_, err = stream.Write([]byte(message + "\n"))
	if err != nil {
		fmt.Println("[DEBUG] Failed to write to stream:", err)
	}
	return err
}

func (cp *Peer) GetConnectedPeers() []peer.ID {
	var peers []peer.ID
	for _, conn := range cp.Host.Network().Conns() {
		remotePeer := conn.RemotePeer()
		if remotePeer != cp.RelayID {
			peers = append(peers, remotePeer)
		}
	}
	fmt.Printf("[DEBUG] Connected peers: %v\n", peers)
	return peers
}

func (cp *Peer) Close() error {
	fmt.Println("[DEBUG] Closing host")
	return cp.Host.Close()
}
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run peer/main.go <relay_address>")
		fmt.Println("Example: go run peer/main.go /ip4/127.0.0.1/tcp/12345/p2p/12D3KooW...")
		os.Exit(1)
	}

	relayAddr := os.Args[1]

	ctx := context.Background()

	fmt.Println("[DEBUG] Creating ChatPeer")
	peer, err := NewPeer(relayAddr)
	if err != nil {
		log.Fatal(err)
	}
	
	defer peer.Close()
	fmt.Println("[DEBUG] Starting ChatPeer")
	if err := peer.ConnectToRelay(ctx); err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("\nCommands:")
	fmt.Println("  connect <peer_address> <nickname> - Connect to a peer")
	fmt.Println("  msg <message> - Send message to all connected peers")
	fmt.Println("  peers - List connected peers")
	fmt.Println("  quit - Exit")

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		parts := strings.SplitN(input, " ", 3)

		switch parts[0] {
		case "connect":
			if len(parts) < 3 {
				fmt.Println("Usage: connect <peer_address>")
				continue
			}
			fmt.Printf("[DEBUG] Command: connect %s \n", parts[1])
			if err := peer.ConnectToPeer(ctx, parts[1]); err != nil {
				fmt.Printf("Failed to connect: %v\n", err)
			}

		case "msg":
			if len(parts) < 2 {
				fmt.Println("Usage: msg <message>")
				continue
			}
			message := strings.Join(parts[1:], " ")
			fmt.Printf("[DEBUG] Command: msg %s\n", message)
			connectedPeers := peer.GetConnectedPeers()

			if len(connectedPeers) == 0 {
				fmt.Println("No peers connected")
				continue
			}

			for _, peerID := range connectedPeers {
				if err := peer.SendMessage(peerID, message); err != nil {
					fmt.Printf("Failed to send to %s: %v\n", peerID, err)
				}
			}

		case "peers":
			fmt.Println("[DEBUG] Command: peers")
			connectedPeers := peer.GetConnectedPeers()
			if len(connectedPeers) == 0 {
				fmt.Println("No peers connected")
			} else {
				fmt.Printf("Connected peers (%d):\n", len(connectedPeers))
				for _, peerID := range connectedPeers {
					
					conns := peer.Host.Network().ConnsToPeer(peerID)
					connType := "direct"
					if len(conns) > 0 && strings.Contains(conns[0].RemoteMultiaddr().String(), "p2p-circuit") {
						connType = "relayed"
					}
					fmt.Printf("  %s - %s connection\n",peerID, connType)
				}
			}

		case "quit":
			fmt.Println("[DEBUG] Command: quit")
			return

		default:
			fmt.Println("Unknown command. Available: connect, msg, peers, quit")
		}
	}
}
