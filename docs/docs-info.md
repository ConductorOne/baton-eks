While developing the connector, please fill out this form. This information is needed to write docs and to help other users set up the connector.

## Connector capabilities

1. What resources does the connector sync?

This connector syncs users and groups from AWS (via baton-id), roles, cluster roles and namespaces.

2. Can the connector provision any resources? If so, which ones? 
No.

## Connector credentials 

1. What credentials or information are needed to set up the connector? (For example, API key, client ID and secret, domain, etc.)
   The following credentials are required:
      global-access-key: User access key 
      global-secret-access-key: User secret access key
      global-region: AWS account region
      assume-role-arn: The role the user will assume to interact with the cluster
      cluster-name: The cluster's name

2. For each item in the list above: 

   * How does a user create or look up that credential or info? Please include links to (non-gated) documentation, screenshots (of the UI or of gated docs), or a video of the process. 

   * Does the credential need any specific scopes or permissions? If so, list them here. 

   * If applicable: Is the list of scopes or permissions different to sync (read) versus provision (read-write)? If so, list the difference here. 

   * What level of access or permissions does the user need in order to create the credentials? (For example, must be a super administrator, must have access to the admin console, etc.)  

To create AWS IAM user access key (includes both parts, access-key and secret-access-key) you can:
   1- In AWS management console,
      Go to IAM service.
      In the left sidebar, click on Users.
      Select the IAM user you want to generate the key for.
      Go to the Security credentials tab.
      Scroll down to Access keys.
      Click Create access key.
      Choose Use cases (these are descriptive cases and do not affect the access key itself) → Click Next.
      Review and click Create access key.

      ⚠️ Copy or download the Access Key ID and Secret Access Key — you won’t be able to view the secret again.
   2- Alternatevily you can use aws cli tool to generate access keys for a user:
   ```bash
   aws iam create-access-key --user-name YOUR_IAM_USER_NAME
   ```
   Example output:
   ```json
   {
    "AccessKey": {
        "UserName": "Bob",
        "Status": "Active",
        "CreateDate": "2015-03-09T18:39:23.411Z",
        "SecretAccessKey": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYzEXAMPLEKEY",
        "AccessKeyId": "AKIAIOSFODNN7EXAMPLE"
    }
   }
   ```

   To identify your AWS account region, in the AWS management top right corner (next to you acocunt id) you will see your region. It may display Global if you are currently in a Global service (such as IAM). Select a service with a region (for example EKS). You can also use the AWS CLI to retrieve your region with the command: 

   ```bash
   aws configure get region
   ```

   The assume-role-arn is the arn of the role to assume for interacting with the cluster. The user must be able to assume such a role. 
   To create a role, go to IAM management console, on the left panel click roles, Create role. Select "Custom trust policy" and add the users allowed to assume the role to the policy:

   ```json
   {
	"Version": "2012-10-17",
	"Statement": [
         {
            "Sid": "Statement1",
            "Effect": "Allow",
               "Principal": {
                  "AWS": "<YOUR_USER_ARN>"
               },
               "Action": "sts:AssumeRole"
         }
      ]
   }
   ```

   You will need to assign permissions policies to the role. You can create your own custom policy or assign a predifined one that has the following permission:

   ```json
   {
    "Version": "2012-10-17",
    "Statement": [
         {
               "Effect": "Allow",
               "Action": [
                  "eks:DescribeCluster"
               ],
               "Resource": "arn:aws:eks:us-east-1:531807593589:cluster/my-rbac-test-cluster"
         }
      ]
   }
   ```
   This is required for the EKS cli tool used by the connector to retrieve the cluster configuration information.
   Once you have created the role, you need to assign it permissions inside the cluster. 
   You can do this by editing the aws-auth configmap and mapping the created role to the cluster-admin user or any cluster user/group with admin privileges. For more restricted, granular permissions, you can create a custom ClusterRole that has read only permissions.

   For explame:

   ```yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
   name: read-only-access
   rules:
   - apiGroups: [""]  # core API group
      resources: ["pods", "services", "configmaps", "namespaces", "secrets", "endpoints", "persistentvolumeclaims"]
      verbs: ["get", "list", "watch"]
   - apiGroups: ["apps", "batch", "extensions", "networking.k8s.io", "rbac.authorization.k8s.io"]
      resources: ["deployments", "replicasets", "statefulsets", "jobs", "cronjobs", "ingresses", "rolebindings", "clusterrolebindings", "roles", "clusterroles"]
      verbs: ["get", "list", "watch"]
   ```
   And bind the cluster role to a user or group via a cluster role binding:

   ```yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRoleBinding
   metadata:
   name: read-only-access-binding
   roleRef:
   apiGroup: rbac.authorization.k8s.io
   kind: ClusterRole
   name: read-only-access
   subjects:
   - kind: Group
      name: read-only-group     # <- this must match the group in aws-auth
      apiGroup: rbac.authorization.k8s.io
   ```

   For the cluster-name go to EKS management console, there you will find a list of all your clusters.