import { test, expect } from '@playwright/test';
import { dependencyLayers, computeDependencyLayout } from '../web-proto/js/regions/canvas/layout.js';
import { listWires } from '../web-proto/js/wires.js';

test('CF-300: listWires and dependencyLayers respect forEach status wires', async () => {
  const doc = {
    apiVersion: "factory.crossplane.io/v1alpha1",
    kind: "Blueprint",
    metadata: { name: "test-foreach-layout" },
    spec: {
      resources: [
        { name: "cluster", type: "Cluster" },
        { name: "nodegroup", type: "NodeGroup", forEach: "resources.cluster.status.subnets" }
      ]
    }
  };

  const wires = listWires(doc);
  const forEachWire = wires.find(w => w.kind === "status" && w.resource === "nodegroup" && w.srcResource === "cluster");
  expect(forEachWire).toBeDefined();

  const layers = dependencyLayers(doc);
  expect(layers["cluster"]).toBe(1);
  expect(layers["nodegroup"]).toBe(2);
});

test('CF-300: forEach object format { over: "..." } emits status wire and elevates layer', async () => {
  const doc = {
    spec: {
      resources: [
        { name: "vpc", type: "VPC" },
        { name: "subnet", type: "Subnet", forEach: { over: "resources.vpc.status.subnets" } }
      ]
    }
  };

  const wires = listWires(doc);
  const wire = wires.find(w => w.kind === "status" && w.resource === "subnet" && w.srcResource === "vpc");
  expect(wire).toBeDefined();
  expect(wire.srcPath).toBe("subnets");
  expect(wire.path).toBe("forEach");

  const layers = dependencyLayers(doc);
  expect(layers["vpc"]).toBe(1);
  expect(layers["subnet"]).toBe(2);
});

test('CF-300: chained dependencies across forEach and field wires calculate topological depths', async () => {
  const doc = {
    spec: {
      resources: [
        { name: "cluster", type: "Cluster" },
        { name: "nodegroup", type: "NodeGroup", forEach: "resources.cluster.status.subnets" },
        { name: "app", type: "App", fields: { clusterName: { from: "resources.nodegroup.status.arn" } } }
      ]
    }
  };

  const layers = dependencyLayers(doc);
  expect(layers["cluster"]).toBe(1);
  expect(layers["nodegroup"]).toBe(2);
  expect(layers["app"]).toBe(3);
});

test('CF-300: param-based forEach does not emit status wire or bump topological layer', async () => {
  const doc = {
    spec: {
      resources: [
        { name: "worker", type: "Worker", forEach: "params.replicas" }
      ]
    }
  };

  const wires = listWires(doc);
  const statusWire = wires.find(w => w.kind === "status" && w.resource === "worker");
  expect(statusWire).toBeUndefined();

  const layers = dependencyLayers(doc);
  expect(layers["worker"]).toBe(1);
});

test('CF-300: computeDependencyLayout places forEach-dependent resource to the right of upstream source', async () => {
  const doc = {
    spec: {
      resources: [
        { name: "cluster", type: "Cluster" },
        { name: "nodegroup", type: "NodeGroup", forEach: "resources.cluster.status.subnets" }
      ]
    }
  };

  const layout = computeDependencyLayout(doc);
  expect(layout.positions["cluster"]).toBeDefined();
  expect(layout.positions["nodegroup"]).toBeDefined();
  // nodegroup must sit strictly to the right of cluster
  expect(layout.positions["nodegroup"].x).toBeGreaterThan(layout.positions["cluster"].x);
});
