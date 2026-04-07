# EasyDo Helm Deployment

This directory contains the Helm deployment materials for the EasyDo platform, using MariaDB as the primary relational database.

## Render manifests

```bash
helm template easydo ./deploy/helm/easydo
```

## Install

```bash
helm upgrade --install easydo ./deploy/helm/easydo -n easydo --create-namespace
```
