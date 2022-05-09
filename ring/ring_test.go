package ring

import (
	"testing"
	"github.com/malwaredllc/minicache/node"
	. "github.com/smartystreets/goconvey/convey"
)

const (
	CONFIG_FILE = "../configs/nodes-docker.json"
)

func TestAddNode(t *testing.T) {
	nodes_config := node.LoadNodesConfig(CONFIG_FILE)
	node0 := nodes_config.Nodes["node0"]
	node1 := nodes_config.Nodes["node1"]
	node2 := nodes_config.Nodes["node2"]
	t.Logf(node0.Group)
	t.Logf(node1.Group)
	t.Logf(node2.Group)
		
	Convey("Given empty ring", t, func() {
		Convey("Then it should add node", func() {
			r := NewRing()
			r.AddNode(node0.Group, node0.Id, node0.Host, node0.RestPort, node0.GrpcPort)

			So(r.Nodes.Len(), ShouldEqual, 1)

			Convey("Then node should've hashed id", func() {
				So(r.Nodes[0].HashId, ShouldHaveSameTypeAs, uint32(0))
			})
		})

		Convey("Then it should add node & sort by node group", func() {
			r := NewRing()
			r.AddNode(node1.Group, node1.Id, node1.Host, node1.RestPort, node1.GrpcPort)
			r.AddNode(node2.Group, node2.Id, node2.Host, node2.RestPort, node2.GrpcPort)

			So(r.Nodes.Len(), ShouldEqual, 2)

			So(node1.HashId, ShouldBeGreaterThan, node2.HashId)

			So(r.Nodes[0].Group, ShouldEqual, "group0")
			So(r.Nodes[1].Group, ShouldEqual, "group1")
		})
	})
}

func TestRemoveNode(t *testing.T) {
	nodes_config := node.LoadNodesConfig(CONFIG_FILE)
	node0 := nodes_config.Nodes["node0"]
	node1 := nodes_config.Nodes["node1"]
	node2 := nodes_config.Nodes["node2"]

	Convey("Given ring with nodes", t, func() {
		r := NewRing()
		r.AddNode(node0.Group, node0.Id, node0.Host, node0.RestPort, node0.GrpcPort)
		r.AddNode(node1.Group, node1.Id, node1.Host, node1.RestPort, node1.GrpcPort)
		r.AddNode(node2.Group, node2.Id, node2.Host, node2.RestPort, node2.GrpcPort)

		Convey("When node doesn't exist", func() {
			Convey("Then it should return error", func() {
				err := r.RemoveNode("nonexistent")
				So(err, ShouldEqual, ErrNodeNotFound)
			})
		})

		Convey("When node exists", func() {
			Convey("Then it should remove node", func() {
				err := r.RemoveNode(node1.Id)
				So(err, ShouldBeNil)

				So(r.Nodes.Len(), ShouldEqual, 2)

				// group 0 hash is lower than group 1, so this is sorted the order they appear in
				So(r.Nodes[0].Id, ShouldEqual, node0.Id)
				So(r.Nodes[1].Id, ShouldEqual, node2.Id)
			})
		})
	})
}

func TestGet(t *testing.T) {
	nodes_config := node.LoadNodesConfig(CONFIG_FILE)
	node0 := nodes_config.Nodes["node0"]
	node1 := nodes_config.Nodes["node1"]
	node2 := nodes_config.Nodes["node2"]
	Convey("Given ring with 1 node", t, func() {
		r := NewRing()
		r.AddNode(node1.Group, node1.Id, node1.Host, node1.RestPort, node1.GrpcPort)

		Convey("Then it should return that node regardless of input", func() {
			insertnode := r.Get("id")
			So(insertnode, ShouldEqual, node1.Id)

			insertnode = r.Get("anykey")
			So(insertnode, ShouldEqual, node1.Id)
		})
	})

	Convey("Given ring with multiple nodes", t, func() {
		insertid := "random_key"

		r := NewRing()
		r.AddNode(node0.Group, node0.Id, node0.Host, node0.RestPort, node0.GrpcPort)
		r.AddNode(node1.Group, node1.Id, node1.Host, node1.RestPort, node1.GrpcPort)
		r.AddNode(node2.Group, node2.Id, node2.Host, node2.RestPort, node2.GrpcPort)

		Convey("Then it should return node closest", func() {
			inserthash := node.HashId(insertid)

			So(inserthash, ShouldBeGreaterThan, node1.HashId)
			So(inserthash, ShouldBeLessThan, node0.HashId)

			insertnode := r.Get(insertid)
			So(insertnode, ShouldEqual, node0.Id)
		})
	})
}