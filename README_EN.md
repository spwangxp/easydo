# EasyDo

EasyDo is an intelligent work platform for team collaboration, focused on core scenarios such as pipeline orchestration, release deployment, resource integration, and execution scheduling. This repository includes the full frontend, backend, agent, and local deployment materials so you can quickly try the platform locally or extend it further.

## Table of Contents

- [Project Introduction](#project-introduction)
- [Core Capabilities Overview](#core-capabilities-overview)
- [Architecture and Modules](#architecture-and-modules)
- [Local Docker Quick Start](#local-docker-quick-start)
- [Screenshots](#screenshots)
- [Project Structure and Extra Docs](#project-structure-and-extra-docs)

## Project Introduction

EasyDo is built around a simple goal: unify the execution chain used by engineering teams in one place. It brings together common DevOps and platform engineering workflows in a single interface, covering pipeline design, task execution, release deployment, and the coordination of workspaces, credentials, resources, and agents, so teams spend less time switching between systems.

If you are new to the repository, you can think of EasyDo as a complete platform sample that can be started locally and evolved further:

- `easydo-frontend` provides the unified web interface
- `easydo-server` handles APIs, state management, and business orchestration
- `easydo-agent` is responsible for task execution, log streaming, and resource-side integration
- `deploy/` provides the Docker-based local startup path

## Core Capabilities Overview

EasyDo currently focuses on the following platform capabilities:

- Pipelines and task orchestration: pipeline list views, DAG design, run tracking, and task status transitions
- Release deployment: delivery-oriented deployment entry points, process tracking, and result visibility
- Workspaces and project collaboration: the foundation for organization and teamwork across multiple workspaces and projects
- Agent execution system: distributed agents for task dispatch, status reporting, and log streaming
- Credential and resource management: unified management for common credential types and resources such as VMs and Kubernetes
- Real-time interaction: both the frontend and agents keep real-time communication with the server through WebSocket
- Statistics and notifications: baseline feedback for run results, event changes, and platform usage

## Architecture and Modules

The repository is organized into frontend, backend, agent, and deployment layers:

- `easydo-frontend`: a Vue 3 application responsible for platform pages, interaction flows, and visual presentation
- `easydo-server`: a Go-based core service responsible for APIs, authentication, data storage, pipelines, tasks, and related business logic
- `easydo-agent`: a Go-based executor component responsible for receiving tasks, controlling execution, and reporting status and logs
- `deploy`: deployment materials for local or integrated environments; the Docker startup path recommended in this README is also grounded here

From a runtime perspective, `easydo-server` acts as the control center, `easydo-frontend` provides the user-facing entry point, and `easydo-agent` handles execution-side task processing. The three work together through APIs and real-time connections.

## Local Docker Quick Start

The recommended local startup entry is the root `Makefile`. Under the hood, it uses `deploy/docker-compose/docker-compose.yml`.

### Prerequisites

- Docker
- Docker Compose / `docker-compose`
- `make`

### Start

```bash
make up
```

### Common Commands

```bash
make status
make logs
make down
```

### Access

- Frontend: `http://localhost:8088`
- Backend API: `http://localhost:8080`

The current startup path already includes the data initialization needed for a basic local experience. If you want to log in directly, you can use the pre-seeded test accounts: `demo / 1qaz2WSX`, `admin / 1qaz2WSX`, `test / 1qaz2WSX`.

## Screenshots

The screenshots below keep only the pages that best represent the platform’s main workflow, so new readers can quickly understand the product shape.

### Login

![Login](screenshots/01-login.png)

### Dashboard

![Dashboard](screenshots/02-dashboard.png)

### Pipeline List

![Pipeline List](screenshots/03-pipeline.png)

### Pipeline Design

![Pipeline Design](screenshots/12-pipeline-design.png)

### Pipeline Run Detail

![Pipeline Run Detail](screenshots/13-pipeline-detail.png)

### Release Deployment

![Release Deployment](screenshots/05-deploy.png)

### Resource Management

![Resource Management](screenshots/16-resources.png)

### Credential Management

![Credential Management](screenshots/11-credentials.png)

## Project Structure and Extra Docs

```text
easydo/
├── easydo-frontend/   # Frontend application
├── easydo-server/     # Backend service
├── easydo-agent/      # Executor component
├── deploy/            # Deployment assets such as Docker Compose
├── screenshots/       # UI screenshots used by the README
├── docs/              # Additional documentation
├── Makefile           # Local startup and common operations
└── README.md
```

If you want to go deeper:

- `deploy/docker-compose/docker-compose.yml`: local Docker startup orchestration file
- `docs/`: additional project design and usage documentation
- `AGENTS.md`: development collaboration conventions for this repository
