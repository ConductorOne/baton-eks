While developing the connector, please fill out this form. This information is needed to write docs and to help other users set up the connector.

## Connector capabilities

1. What resources does the connector sync?

This connector syncs users, groups from AWS (via baton-id), IAM roles, roles, cluster roles and namespaces.

2. Can the connector provision any resources? If so, which ones? 
No.

## Connector credentials 

1. What credentials or information are needed to set up the connector? (For example, API key, client ID and secret, domain, etc.)

For c1:
   Assume role ARN: The arn of the role the connector will assume
   External ID: Provided by c1, we'll need it for the integration with AWS.
   Cluster name: The name of the cluster
   Region: The region of the AWS account the cluster belongs to.

   This connector works with AWS Baton-ID. You'll be asked

For on Prem (service mode):
   The following credentials are required:
      eks-access-key: User access key 
      eks-secret-access-key: User secret access key
      eks-region: AWS account region
      role-arn: The role the user will assume to interact with the cluster
      eks-cluster-name: The cluster's name

2. For each item in the list above: 

   * How does a user create or look up that credential or info? Please include links to (non-gated) documentation, screenshots (of the UI or of gated docs), or a video of the process.

   * Does the credential need any specific scopes or permissions? If so, list them here. 

   * If applicable: Is the list of scopes or permissions different to sync (read) versus provision (read-write)? If so, list the difference here. 

   * What level of access or permissions does the user need in order to create the credentials? (For example, must be a super administrator, must have access to the admin console, etc.)  

   For the cluster-name go to EKS management console, there you will find a list of all your clusters.

To create an IAM role to assume we can follow the same [steps](https://www.conductorone.com/docs/baton/aws-v2/#create-an-aws-iam-role-for-conductorone-to-use) described for seting up an AWS connector. We login to our AWS account, go to IAM, under Access Management tab in the left side menu click on Roles. On the right side, click on Create Role.
Select Custom Trust Policy and paste the following into the Trust Policy JSON editor, replacing EXTERNAL_ID_FROM_C1_INTEGRATIONS_PAGE with the External ID from ConductorOne.

```json
{

  "Version": "2012-10-17",

  "Statement": [

    {

      "Effect": "Allow",

      "Principal": {

        "AWS": "arn:aws:iam::765656841499:role/ConductorOneService"

      },

      "Action": "sts:AssumeRole",

      "Condition": {

        "StringEquals": {

          "sts:ExternalId": "EXTERNAL_ID_FROM_C1_INTEGRATIONS_PAGE"

        }

      }

    }

  ]

}
```
Once you have created the role, we need to add the necessary permissions. Select your newly created role.
Under Permissions Policies, click Add Permissions and select Create Inline Policy. Select JSON and paste the following permissions. 

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Action": [
                "iam:AttachRolePolicy",
                "iam:GetRole",
                "iam:ListRoles",
                "iam:PassRole",
                "iam:GetPolicy",
                "iam:ListAttachedRolePolicies",
                "iam:ListRolePolicies",
                "iam:TagRole",
                "iam:GetOpenIDConnectProvider",
                "iam:ListOpenIDConnectProviders",
                "iam:TagOpenIDConnectProvider",
                "iam:GetContextKeysForPrincipalPolicy",
                "iam:SimulatePrincipalPolicy"
            ],
            "Effect": "Allow",
            "Resource": "*",
            "Sid": "ConductorOneReadAccess"
        },
        {
            "Sid": "AccessToSSOProvisionedRoles",
            "Effect": "Allow",
            "Action": [
                "iam:AttachRolePolicy",
                "iam:GetRole",
                "iam:ListAttachedRolePolicies",
                "iam:ListRolePolicies"
            ],
            "Resource": "arn:aws:iam::*:role/aws-reserved/sso.amazonaws.com/*"
        },
        {
            "Sid": "IAMListPermissions",
            "Effect": "Allow",
            "Action": [
                "iam:ListRoles",
                "iam:ListPolicies",
                "iam:ListAccessKeys"
            ],
            "Resource": "*"
        },
        {
            "Effect": "Allow",
            "Action": [
                "eks:DescribeCluster",
                "eks:DescribeClusterVersions",
                "eks:DescribeNodegroup",
                "eks:AssociateIdentityProviderConfig",
                "eks:ListTagsForResource",
                "eks:ListClusters",
                "eks:ListNodegroups",
                "eks:AccessKubernetesApi",
                "eks:AssociateAccessPolicy",
                "eks:ListAssociatedAccessPolicies",
                "eks:AssociateIdentityProviderConfig",
                "eks:DescribeAccessEntry",
                "eks:ListAccessEntries",
                "eks:ListAccessPolicies",
                "eks:DescribeAddon",
                "eks:DescribeAddonVersions",
                "eks:DescribeAddonConfiguration"
            ],
            "Resource": "*"
        }
    ]
}
```

#### For Service mode you we'll need access key and secret:
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

   The role-arn is the arn of the role to assume for interacting with the cluster. The user must be able to assume such a role. 
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
   
   ### Cluster side

   Once you have created the role, you need to assign it permissions inside the cluster. 
   You can do this by editing the aws-auth configmap and mapping the created role to the cluster-admin user or any cluster user/group with admin privileges. For more restricted, granular permissions, you can create a custom ClusterRole that has read only permissions.

   ### Prerequisites
    You need the AWS CLI installed and configured, and kubectl configured to connect to your EKS cluster


You can see the current state of your aws-auth configmap using

```bash
kubectl get configmap -n kube-system aws-auth -o yaml
```
Fetch the current state and save it to a file

   ```bash
kubectl get configmap -n kube-system aws-auth -o yaml > aws-auth-full.yaml
```

Edit the file in an editor of your choice adding the corresponding mappings in the mapRoles section

```yaml
apiVersion: v1
data:
  mapRoles: |
    - groups:
      - test-group-1
      rolearn: arn:aws:iam::1234567:role/ExampleEntry
      username: example-role-username
#   Add you new entry here
    # - rolearn: YOUR_ROLE_ARN
    #   username: any unique name, can be YOUR_ROLE_ARN
    #   groups:
    #   - readers
  mapUsers: |
    - groups:
      - test-group-1
      userarn: arn:aws:iam::1234567:user/test-user
      username: test-user-1
kind: ConfigMap
metadata:
  name: aws-auth
  namespace: kube-system
```

WARNING: This action replaces the original aws-auth configmap. Be sure to check your changes before applying them. 

```bash
kubectl apply -f aws-auth-full.yaml
```

Create a cluster role. This example has the permissions to read-only all resources from the cluster.

  ```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: read-only-access-cluster-role
rules:
- apiGroups: [""]
  resources: ["pods", "services", "configmaps", "namespaces", "secrets", "endpoints", "persistentvolumeclaims", "serviceaccounts"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["apps", "batch", "extensions", "networking.k8s.io", "rbac.authorization.k8s.io"]
  resources: ["deployments", "replicasets", "statefulsets", "jobs", "cronjobs", "ingresses", "rolebindings", "clusterrolebindings", "roles", "clusterroles"]
  verbs: ["get", "list", "watch"]
   ```

Apply the new cluster role. This action adds the role to the cluster.

```bash
kubectl apply -f reader-role.yaml
```

Now lets create the cluster role binding. This binds a kubernetes group or user to a ClusterRole.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: reader-role-binding
subjects:
- kind: Group
  name: readers # <- The name of the group you mapped the ARN role to in the configmap
  apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: ClusterRole
  name: read-only-access # <- Name of the cluster role you created
  apiGroup: rbac.authorization.k8s.io
```
And apply the yaml to the cluster

```bash
kubectl apply -f reader-role-binding.yaml
```

Keep in mind that cluster role binding may take some time to propagate in the cluster, be patient.
Done!
