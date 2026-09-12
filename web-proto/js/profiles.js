/**
 * Kind profiles: what a drop writes onto the canvas (starter) and what the
 * inspector shows first (essentials). Drop, the Scaffold button and the
 * essentials form all read this module, so the three cannot disagree.
 *
 * Starter values follow the Kubernetes references consulted in the design
 * doc: pinned image tag (never :latest), containerPort matching the image,
 * cpu+memory requests, a memory limit only, replicas >= 2.
 */

export const STARTER_IMAGE = "nginx:1.27";
export const STARTER_PORT = "80";

const WORKLOADS = ["Deployment", "StatefulSet", "DaemonSet"];
const BATCH = ["Job", "CronJob"];

export function isWorkloadKind(kind) {
  return WORKLOADS.indexOf(kind) !== -1 || BATCH.indexOf(kind) !== -1;
}

/** Path prefix of the first container for a kind. */
export function containerPrefix(kind) {
  return kind === "CronJob"
    ? "spec.jobTemplate.spec.template.spec.containers[0]"
    : "spec.template.spec.containers[0]";
}

/** Path prefix of the pod template for a kind. */
export function podTemplatePrefix(kind) {
  return kind === "CronJob" ? "spec.jobTemplate.spec.template" : "spec.template";
}

function containerStarter(kind, name) {
  const c = containerPrefix(kind);
  const f = {};
  f[c + ".name"] = { value: name };
  f[c + ".image"] = { value: STARTER_IMAGE };
  f[c + ".ports[0].containerPort"] = { value: STARTER_PORT };
  f[c + ".resources.requests[cpu]"] = { value: "100m" };
  f[c + ".resources.requests[memory]"] = { value: "128Mi" };
  f[c + ".resources.limits[memory]"] = { value: "256Mi" };
  return f;
}

function starterFor(kind, name) {
  const f = {};
  if (WORKLOADS.indexOf(kind) !== -1) {
    if (kind !== "DaemonSet") f["spec.replicas"] = { value: "2" };
    f["spec.selector.matchLabels[app]"] = { value: name };
    f["spec.template.metadata.labels[app]"] = { value: name };
    if (kind === "StatefulSet") f["spec.serviceName"] = { value: name };
    Object.assign(f, containerStarter(kind, name));
    return f;
  }
  if (kind === "Job") {
    f["spec.template.metadata.labels[app]"] = { value: name };
    Object.assign(f, containerStarter(kind, name));
    delete f[containerPrefix(kind) + ".ports[0].containerPort"];
    f["spec.template.spec.restartPolicy"] = { value: "Never" };
    return f;
  }
  if (kind === "CronJob") {
    f["spec.schedule"] = { value: "*/5 * * * *" };
    Object.assign(f, containerStarter(kind, name));
    delete f[containerPrefix(kind) + ".ports[0].containerPort"];
    f["spec.jobTemplate.spec.template.spec.restartPolicy"] = { value: "Never" };
    return f;
  }
  if (kind === "Service") {
    f["spec.selector[app]"] = { value: name };
    f["spec.ports[0].port"] = { value: "80" };
    f["spec.ports[0].targetPort"] = { value: "80" };
    f["spec.type"] = { value: "ClusterIP" };
    return f;
  }
  return null;
}

/**
 * Essentials rows. kind: "app-label" (writes selector + template labels),
 * "field" (one path), "env" (repeater over <prefix>[i].name/.value).
 * `param` is the XRD parameter name "expose" creates.
 */
function essentialsFor(kind) {
  const c = containerPrefix(kind);
  if (WORKLOADS.indexOf(kind) !== -1 || BATCH.indexOf(kind) !== -1) {
    const rows = [];
    if (WORKLOADS.indexOf(kind) !== -1) rows.push({ kind: "app-label", label: "App label" });
    if (kind === "Deployment" || kind === "StatefulSet") {
      rows.push({ kind: "field", path: "spec.replicas", label: "Replicas", type: "integer", param: "replicas" });
    }
    if (kind === "CronJob") {
      rows.push({ kind: "field", path: "spec.schedule", label: "Schedule", type: "string", param: "schedule" });
    }
    rows.push({ kind: "field", path: c + ".image", label: "Image", type: "string", param: "image", placeholder: STARTER_IMAGE });
    rows.push({ kind: "field", path: c + ".name", label: "Container name", type: "string" });
    if (WORKLOADS.indexOf(kind) !== -1) {
      rows.push({ kind: "field", path: c + ".ports[0].containerPort", label: "Port", type: "integer", param: "containerPort" });
    }
    rows.push({ kind: "field", path: c + ".resources.requests[cpu]", label: "CPU request", type: "string", param: "cpuRequest" });
    rows.push({ kind: "field", path: c + ".resources.requests[memory]", label: "Memory request", type: "string", param: "memoryRequest" });
    rows.push({ kind: "field", path: c + ".resources.limits[memory]", label: "Memory limit", type: "string", param: "memoryLimit" });
    rows.push({ kind: "env", prefix: c + ".env", label: "Environment variables" });
    return rows;
  }
  if (kind === "Service") {
    return [
      { kind: "service-selector", label: "Target app" },
      { kind: "field", path: "spec.ports[0].port", label: "Port", type: "integer", param: "servicePort" },
      { kind: "field", path: "spec.ports[0].targetPort", label: "Target port", type: "integer", param: "targetPort" },
      { kind: "field", path: "spec.type", label: "Type", type: "string", enum: ["ClusterIP", "NodePort", "LoadBalancer"] },
    ];
  }
  return null;
}

/** @returns {{starter:(name:string)=>Object, essentials:Array}|null} */
export function profileFor(kind, provider) {
  if (provider && provider !== "k8s") return null;
  const ess = essentialsFor(kind);
  const hasStarter = starterFor(kind, "x") !== null;
  if (!ess && !hasStarter) return null;
  return {
    starter: function (name) { return starterFor(kind, name) || {}; },
    essentials: ess || [],
  };
}
